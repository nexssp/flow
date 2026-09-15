package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aiflow "github.com/nexssp/ai/flow"
	"github.com/nexssp/cost"
	"github.com/nexssp/flow"
	"github.com/nexssp/flow/runner/capability"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"
)

type Request struct {
	Path    string
	Payload map[string]any
	Args    []string

	Verbosity    int
	Info         bool
	Assertions   []string
	Metrics      bool
	OutFormat    string
	OutDir       string
	BenchNode    string
	BenchRuns    int
	CacheDir     string
	ApprovalMode ApprovalMode

	Resume string
	Store  CheckpointStore

	Stdout io.Writer
	Stderr io.Writer
}

func RunFlow(ctx context.Context, req Request) int {
	stdout := req.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	observer := NewRunnerObserver(stdout, req.Verbosity)

	reg, err := BuildStaticRegistryWithObserver(observer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ failed to build capability registry: %v\n", err)

		return 1
	}

	return RunWithRegistry(ctx, req, reg, observer)
}

func RunWithCustomRegistry(ctx context.Context, req Request, reg *flow.MapRegistry) int {
	stdout := req.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	observer := NewRunnerObserver(stdout, req.Verbosity)

	return RunWithRegistry(ctx, req, reg, observer)
}

func RunWithRegistry(ctx context.Context, req Request, reg *flow.MapRegistry, observer *RunnerObserver) int {
	stdout := req.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := req.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if observer == nil {
		observer = NewRunnerObserver(stdout, req.Verbosity)
	}

	rArgs := parseRunnerArgs(req.Args)

	useInfo := rArgs.Info || req.Info

	assertions := rArgs.Assertions
	if len(assertions) == 0 {
		assertions = req.Assertions
	}

	if useInfo {
		// contextcheck fix: forward ctx so WASM compilation can be cancelled.
		return PrintFlowInfo(ctx, stdout, req)
	}

	pre, err := flow.Preprocess(req.Path)
	if err != nil {
		fmt.Fprintf(stderr, "❌ failed to preprocess flow: %v\n", err)

		return 1
	}

	manifestStr := pre.DSL

	allAssertions := mergeAssertions(manifestStr, assertions)

	resolved := flow.ResolveConfig(manifestStr, req.Args)
	cfg := resolved.Config

	if resolved.Provenance["verbosity"] == flow.LayerDefault && req.Verbosity > 0 {
		cfg.Verbosity = req.Verbosity
		resolved.Provenance["verbosity"] = flow.LayerCLI
	}

	if resolved.Provenance["output_format"] == flow.LayerDefault && req.OutFormat != "" {
		cfg.OutputFormat = req.OutFormat
	}

	if resolved.Provenance["output_dir"] == flow.LayerDefault && req.OutDir != "" {
		cfg.OutputDir = req.OutDir
	}

	observer.SetVerbosity(cfg.Verbosity)

	appMode, err := parseApprovalMode(cfg.Approval)
	if err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)

		return 2
	}

	if req.ApprovalMode != "" && resolved.Provenance["approval"] == flow.LayerDefault {
		appMode = req.ApprovalMode
	}

	ledger := cost.NewLedger(cfg.BudgetMicros, cost.USD)

	now := time.Now().UTC()
	border := strings.Repeat("═", 78)

	fmt.Fprintln(stdout, border)
	fmt.Fprintf(stdout, "  NEXSS FLOW RUNNER  •  %s UTC\n", now.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(stdout, "  flow      : %s\n", req.Path)
	fmt.Fprintf(stdout, "  config    : verbosity=%d  budget=$%.6f  approval=%s",
		cfg.Verbosity, float64(cfg.BudgetMicros)/1_000_000.0, cfg.Approval)

	if cfg.MaxTokens > 0 {
		fmt.Fprintf(stdout, "  max_tokens=%d", cfg.MaxTokens)
	}

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, border)
	fmt.Fprintln(stdout)

	dsl := flow.SanitizeDSL(manifestStr)
	if dsl == "" {
		fmt.Fprintf(stderr, "❌ no executable pipeline found in %s\n", req.Path)

		return 1
	}

	store := req.Store
	if store == nil {
		if fs, err := NewFileCheckpointStore(".nexss/runs"); err == nil {
			store = fs
		} else {
			fmt.Fprintf(stderr, "⚠️  checkpoint store disabled: %v\n", err)
		}
	}

	currentHash := flowHash(dsl)

	resumeID := rArgs.Resume
	if resumeID == "" {
		resumeID = req.Resume
	}

	var (
		runID       string
		resumeState map[string]any
	)

	if resumeID != "" {
		if store == nil {
			fmt.Fprintf(stderr, "❌ --resume requires a checkpoint store\n")

			return 1
		}

		cp, found, err := store.Load(ctx, resumeID)
		if err != nil {
			fmt.Fprintf(stderr, "❌ resume: %v\n", err)

			return 1
		}

		if !found {
			fmt.Fprintf(stderr, "❌ resume: checkpoint %q not found\n", resumeID)

			return 1
		}

		if cp.FlowHash != currentHash {
			fmt.Fprintf(stderr,
				"❌ resume: flow changed since checkpoint %q\n"+
					"   checkpoint hash : %s\n"+
					"   current hash    : %s\n",
				cp.RunID, cp.FlowHash, currentHash)

			return 1
		}

		runID = fmt.Sprintf("%s_resume_%d", cp.RunID, time.Now().UnixNano())
		resumeState = cp.State
		observer.AddSpend(cp.SpentMicros)

		if cfg.Verbosity >= 1 {
			tNow := time.Now().Format("15:04:05.000")
			fmt.Fprintf(stdout,
				"[%s] ⚙ resume     from %s at layer %d (%d outputs restored)\n",
				tNow, cp.RunID, cp.Layer, len(cp.State))
		}
	} else {
		runID = fmt.Sprintf("run_%d", time.Now().UnixNano())
	}

	if store != nil {
		onLayer := func(layerCtx context.Context, layer int, state *dag.State) error {
			saveCtx := context.WithoutCancel(layerCtx)

			return store.Save(saveCtx, Checkpoint{
				RunID:       runID,
				Flow:        req.Path,
				FlowHash:    currentHash,
				Layer:       layer,
				SavedAt:     time.Now().UTC(),
				SpentMicros: observer.TotalSpentMicros(),
				State:       state.Data(),
			})
		}
		ctx = dag.WithLayerCallback(ctx, onLayer)
	}

	if cfg.Verbosity >= 3 {
		tNow := time.Now().Format("15:04:05.000")
		fmt.Fprintf(stdout, "[%s] ⚙ parse      .flow loaded, %d B\n", tNow, len(manifestStr))
		fmt.Fprintf(stdout,
			"[%s] ⚙ config     verbosity=%d(%s)  budget=%d(%s)  max_tokens=%d(%s)  approval=%s(%s)\n",
			tNow,
			cfg.Verbosity, resolved.Provenance["verbosity"],
			cfg.BudgetMicros, resolved.Provenance["budget"],
			cfg.MaxTokens, resolved.Provenance["max_tokens"],
			cfg.Approval, resolved.Provenance["approval"])
	}

	if len(pre.Pipelines) > 0 {
		if err := flow.RegisterPipelines(reg, pre.Pipelines); err != nil {
			fmt.Fprintf(stderr, "❌ %v\n", err)

			return 1
		}

		if cfg.Verbosity >= 2 {
			tNow := time.Now().Format("15:04:05.000")

			names := make([]string, 0, len(pre.Pipelines))
			for _, p := range pre.Pipelines {
				names = append(names, p.Name)
			}

			sort.Strings(names)
			fmt.Fprintf(stdout, "[%s] ⚙ pipelines %s\n", tNow, strings.Join(names, " "))
		}
	}

	if cfg.Verbosity >= 3 {
		tNow := time.Now().Format("15:04:05.000")
		fmt.Fprintf(stdout, "[%s] ⚙ compile    pipeline ready\n", tNow)
	}

	// contextcheck fix: forward ctx so WASM compilation can be cancelled.
	resolver, err := capability.NewResolver(ctx, reg, manifestStr)
	if err == nil {
		defer resolver.Close()

		resolver.Install()
	}

	obsHook := observe.Hook(observer)
	liveHook := observer.Hook()

	approvalGate := NewApprovalGate(appMode)
	compiler := flow.NewCompiler(reg,
		flow.WithApprovalGate(approvalGate),
		flow.WithReserver(ledger),
		flow.WithHooks(obsHook, liveHook),
	)
	execAct := flow.NewExecuteAction(compiler).ToBuilder().AnyHook(obsHook, liveHook).Build()

	runCtx, scope, release := xctx.NewScope(ctx)
	defer release()

	scope.ExecutionID = runID

	if cfg.MaxTokens > 0 {
		runCtx = aiflow.WithMaxTokens(runCtx, cfg.MaxTokens)
	}

	payload := make(map[string]any, len(req.Payload)+len(resumeState))
	for k, v := range req.Payload {
		payload[k] = v
	}

	for k, v := range resumeState {
		payload[k] = v
	}

	start := time.Now()
	res, err := execAct.Do(runCtx, flow.GraphExecReq{
		DSL:            dsl,
		InitialPayload: payload,
	})
	duration := time.Since(start)

	showSummary := observer.Verbosity() >= 1 || req.Metrics || err != nil

	if err != nil {
		var execErr *dag.ExecutionError
		if errors.As(err, &execErr) && execErr.State != nil {
			if store != nil {
				saveCtx := context.WithoutCancel(ctx)
				_ = store.Save(saveCtx, Checkpoint{
					RunID:       runID,
					Flow:        req.Path,
					FlowHash:    currentHash,
					Layer:       execErr.Layer,
					SavedAt:     time.Now().UTC(),
					SpentMicros: observer.TotalSpentMicros(),
					State:       execErr.State.Data(),
				})
			}

			execErr.State.Release()
		}

		fmt.Fprintf(stdout, "\n✗ FLOW FAILED after %v\n", duration.Round(time.Millisecond))

		if store != nil {
			fmt.Fprintf(stdout, "   resume with: %s\n",
				buildResumeCommand(req.Path, req.Payload, req.Args, runID))
		}

		if showSummary {
			observer.PrintSummary(stdout)
		}

		return 1
	}

	outJSON, _ := json.MarshalIndent(res, "", "  ")
	fmt.Fprintf(stdout, "\n✅ FLOW COMPLETED after %v\nOutputs:\n%s\n",
		duration.Round(time.Millisecond), outJSON)

	if cfg.OutputFormat != "" {
		saveOutput(cfg.OutputDir, cfg.OutputFormat, runID, res, stdout)
	}

	if store != nil {
		_ = store.Delete(context.WithoutCancel(ctx), runID)
	}

	if showSummary {
		observer.PrintSummary(stdout)
	}

	return RunAssertions(res, allAssertions, observer)
}

func saveOutput(dir, format, execID string, data any, out io.Writer) {
	if dir == "" {
		dir = ".runs"
	}

	_ = os.MkdirAll(dir, 0o755)

	ts := time.Now().Format("20060102_150405")
	filename := filepath.Join(dir, fmt.Sprintf("run_%s_%s.%s", ts, execID, format))

	var b []byte
	if format == "json" {
		b, _ = json.MarshalIndent(data, "", "  ")
	} else {
		b = []byte(fmt.Sprintf("%v", data))
	}

	if err := os.WriteFile(filename, b, 0o600); err == nil {
		fmt.Fprintf(out, "💾 Output saved to: %s\n", filename)
	}
}

func mergeAssertions(manifest string, extra []string) []string {
	var out []string

	seen := make(map[string]bool)

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}

		seen[s] = true
		out = append(out, s)
	}

	for _, line := range strings.Split(manifest, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@assert:") {
			add(strings.TrimPrefix(trimmed, "@assert:"))
		}
	}

	for _, a := range extra {
		add(a)
	}

	return out
}

func buildResumeCommand(path string, payload map[string]any, args []string, runID string) string {
	exe := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")

	parts := make([]string, 0, len(args)+4)
	parts = append(parts, exe, shellQuote(path))

	if len(payload) > 0 {
		if data, err := json.Marshal(payload); err == nil && len(data) <= 256 {
			parts = append(parts, shellQuote(string(data)))
		}
	}

	for _, a := range args {
		if strings.HasPrefix(a, "--resume=") {
			continue
		}

		parts = append(parts, a)
	}

	parts = append(parts, "--resume="+runID)

	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$&|;<>(){}[]*?") {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
