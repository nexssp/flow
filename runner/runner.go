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

	"github.com/nexssp/cost"
	"github.com/nexssp/flow"
	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/runner/capability"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
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

func RunWithRegistry(ctx context.Context, req Request, reg *action.Registry, observer *RunnerObserver) int {
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
		return PrintFlowInfo(ctx, stdout, req, reg)
	}

	pre, err := flow.Preprocess(req.Path)
	if err != nil {
		fmt.Fprintf(stderr, "❌ failed to preprocess flow: %v\n", err)

		return 1
	}

	manifestStr := pre.DSL

	reg, err = materializeFromPreprocessed(pre, reg)
	if err != nil {
		fmt.Fprintf(stderr, "❌ materialize declarations: %v\n", err)
		return 1
	}

	reg, err = flow.ContributeRegistry(pre, reg)
	if err != nil {
		fmt.Fprintf(stderr, "❌ directive contribution: %v\n", err)
		return 1
	}

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

	if cfg.Verbosity >= 1 {
		fmt.Fprintln(stdout, border)
		fmt.Fprintf(stdout, "  NEXSS FLOW RUNNER  •  %s UTC\n", now.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(stdout, "  flow      : %s\n", req.Path)
		fmt.Fprintf(stdout, "  config    : verbosity=%d  budget=$%.6f  approval=%s",
			cfg.Verbosity, float64(cfg.BudgetMicros)/1_000_000.0, cfg.Approval)
		if cfg.MaxTokens > 0 {
			fmt.Fprintf(stdout, "  max_tokens=%d", cfg.MaxTokens)
		}
		if pre.Profile != "" {
			fmt.Fprintf(stdout, "  profile=%s", pre.Profile)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, border)
		fmt.Fprintln(stdout)
	}

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

		if pre.Profile != "" {
			fmt.Fprintf(stdout, "[%s] ⚙ profile    %s\n", tNow, pre.Profile)
		}
	}

	if len(pre.Pipelines) > 0 {
		var regErr error
		reg, regErr = flow.RegisterPipelines(reg, pre.Pipelines)
		if regErr != nil {
			fmt.Fprintf(stderr, "❌ %v\n", regErr)

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

	resolver, err := capability.NewResolver(ctx, reg, manifestStr)
	if err == nil {
		defer resolver.Close()

		if remoteActions := resolver.Actions(); len(remoteActions) > 0 {
			merged, mergeErr := action.NewRegistry(
				action.Library{Name: "base", Actions: reg.Actions()},
				action.Library{Name: "remote", Actions: remoteActions},
			)
			if mergeErr != nil {
				fmt.Fprintf(stderr, "❌ capability merge: %v\n", mergeErr)

				return 1
			}
			reg = merged
		}
	}

	declaredHooks := make([]action.AnyHook, 0, len(pre.Hooks))
	for _, hd := range pre.Hooks {
		hook, ok := action.NamedHook(hd.Name)
		if !ok {
			fmt.Fprintf(stderr,
				"❌ hook %q is not registered (declared at %s)\n",
				hd.Name, hd.Pos)
			return 1
		}
		declaredHooks = append(declaredHooks, hook)
	}

	obsHook := observe.Hook(observer)
	liveHook := observer.Hook()

	approvalGate := NewApprovalGate(appMode)
	compiler := flow.NewCompiler(reg,
		flow.WithApprovalGate(approvalGate),
		flow.WithReserver(ledger),
		flow.WithHooks(obsHook, liveHook),
		flow.WithHooks(declaredHooks...),
		flow.WithSecurityObserver(observer),
	)
	execAct := flow.NewExecuteAction(compiler).ToBuilder().AnyHook(obsHook, liveHook).Build()

	runCtx, scope, release := xctx.NewScope(ctx)
	defer release()

	scope.ExecutionID = runID

	// Inherit caller's context scope (roles, permissions, features, identity)
	if parentScope := xctx.ScopeFrom(ctx); parentScope != nil {
		if len(parentScope.Roles) > 0 {
			scope.Roles = append([]string(nil), parentScope.Roles...)
			scope.Role = parentScope.Role
		}
		if len(parentScope.Permissions) > 0 {
			scope.Permissions = append([]string(nil), parentScope.Permissions...)
		}
		if len(parentScope.Features) > 0 {
			scope.Features = append([]string(nil), parentScope.Features...)
		}
		if parentScope.UserID != "" {
			scope.UserID = parentScope.UserID
		}
		if parentScope.TenantID != "" {
			scope.TenantID = parentScope.TenantID
		}
	}

	// Propagate registry and compiler to the execution context
	runCtx = contracts.WithRegistry(runCtx, reg)
	runCtx = contracts.WithCompiler(runCtx, compiler)

	if len(pre.Pools) > 0 {
		pools := make(map[string][]string, len(pre.Pools))
		for _, p := range pre.Pools {
			pools[p.Name] = p.Members
		}
		runCtx = contracts.WithPools(runCtx, pools)

		if cfg.Verbosity >= 2 {
			tNow := time.Now().Format("15:04:05.000")
			for _, p := range pre.Pools {
				fmt.Fprintf(stdout,
					"[%s] ⚙ pool       %s = [%s] (%s)\n",
					tNow, p.Name, strings.Join(p.Members, ", "), p.Pos)
			}
		}
	}

	if cfg.MaxTokens > 0 {
		runCtx = flow.WithMaxTokens(runCtx, cfg.MaxTokens)
	}

	payload := make(map[string]any, len(req.Payload)+len(resumeState))
	for k, v := range req.Payload {
		payload[k] = v
	}

	for k, v := range resumeState {
		payload[k] = v
	}

	if rArgs.Approve != "" {
		runCtx = xctx.WithApprovalToken(runCtx, rArgs.Approve)
		if cfg.Verbosity >= 1 {
			tNow := time.Now().Format("15:04:05.000")
			fmt.Fprintf(stdout, "[%s] ⚙ approve    token=%s\n", tNow, rArgs.Approve)
		}
	} else if tok := xctx.ApprovalTokenFrom(ctx); tok != "" {
		runCtx = xctx.WithApprovalToken(runCtx, tok)
	}

	start := time.Now()
	res, err := execAct.Do(runCtx, flow.GraphExecReq{
		DSL:            dsl,
		Profile:        flow.Profile(pre.Profile),
		Name:           req.Path,
		InitialPayload: payload,
		Preprocessed:   pre,
	})
	duration := time.Since(start)

	showSummary := observer.Verbosity() >= 1 || req.Metrics || err != nil

	if err != nil && errors.Is(err, dag.ErrSuspended) {
		return handleSuspension(ctx, req, stderr, stdout, observer, store,
			runID, currentHash, payload, duration, err)
	}

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

		appErr := xerr.From(err)
		fmt.Fprintf(stdout, "✗ %s\n", appErr.Message)

		if cfg.Verbosity >= 1 {
			fmt.Fprintf(stdout, "  kind    : %s\n", appErr.Kind)
			if execErr != nil && execErr.FailedNode != "" {
				fmt.Fprintf(stdout, "  node    : %s\n", execErr.FailedNode)
			}
			fmt.Fprintf(stdout, "  after   : %v\n", duration.Round(time.Millisecond))
		}

		if cfg.Verbosity >= 2 {
			if appErr.Cause != nil {
				fmt.Fprintf(stdout, "  cause   : %v\n", appErr.Cause)
			}
			observer.PrintSummary(stdout)
		}

		return 1
	}

	if cfg.Verbosity >= 1 {
		outJSON, _ := json.MarshalIndent(res, "", "  ")
		fmt.Fprintf(stdout, "✓ flow completed in %v\n", duration.Round(time.Millisecond))
		fmt.Fprintf(stdout, "  name    : %s\n", req.Path)
		fmt.Fprintf(stdout, "  layers  : %d\n", res.LayersRun)
		fmt.Fprintf(stdout, "  result  :\n%s\n", indentJSON(res.Result))
		if cfg.Verbosity >= 2 {
			fmt.Fprintf(stdout, "  outputs :\n%s\n", outJSON)
		}
	} else {
		fmt.Fprintln(stdout, compactJSON(res.Result))
	}

	if cfg.OutputFormat != "" {
		saveOutput(cfg.OutputDir, cfg.OutputFormat, runID, res, stdout)
	}

	if store != nil {
		_ = store.Delete(context.WithoutCancel(ctx), runID)
	}

	if showSummary {
		observer.PrintSummary(stdout)
	}

	return RunAssertions(res, allAssertions, observer, cfg.Verbosity)
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

// handleSuspension saves the checkpoint, prints the suspension message
// and the exact resume command, and returns exit code 3.
func handleSuspension(
	ctx context.Context,
	req Request,
	stderr, stdout io.Writer,
	observer *RunnerObserver,
	store CheckpointStore,
	runID, flowHash string,
	payload map[string]any,
	duration time.Duration,
	suspErr error,
) int {
	_ = stderr

	if store != nil {
		saveCtx := context.WithoutCancel(ctx)
		_ = store.Save(saveCtx, Checkpoint{
			RunID:       runID,
			Flow:        req.Path,
			FlowHash:    flowHash,
			SavedAt:     time.Now().UTC(),
			SpentMicros: observer.TotalSpentMicros(),
			State:       payload,
			Suspended:   true,
		})
	}

	var susp *dag.SuspendError
	_ = errors.As(suspErr, &susp)

	fmt.Fprintf(stdout, "\n⏸ FLOW SUSPENDED after %v — awaiting approval\n",
		duration.Round(time.Millisecond))

	gate := ""
	if susp != nil {
		if m, ok := susp.Payload.(map[string]any); ok {
			if g, ok := m["gate"].(string); ok {
				gate = g
			}
			if role, ok := m["role"].(string); ok && role != "" {
				fmt.Fprintf(stdout, "   role   : %s\n", role)
			}
			if reason, ok := m["reason"].(string); ok && reason != "" {
				fmt.Fprintf(stdout, "   reason : %s\n", reason)
			}
			if expires, ok := m["expires"].(string); ok && expires != "" {
				fmt.Fprintf(stdout, "   expires: %s\n", expires)
			}
		} else if susp.Reason != "" {
			fmt.Fprintf(stdout, "   reason : %s\n", susp.Reason)
		}
	}

	if store != nil {
		approveArg := gate
		if approveArg == "" {
			approveArg = "all"
		}
		fmt.Fprintf(stdout, "   resume : %s\n",
			buildResumeApproveCommand(req.Path, req.Args, runID, approveArg))
	}

	if observer.Verbosity() >= 1 {
		observer.PrintSummary(stdout)
	}
	return 3
}

// buildResumeApproveCommand emits the exact shell command that resumes
// a suspended flow with the approval token installed. The gate name is
// passed to --approve; users may also pass --approve=all.
func buildResumeApproveCommand(path string, args []string, runID, gate string) string {
	exe := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")

	parts := []string{exe, shellQuote(path)}
	for _, a := range args {
		if strings.HasPrefix(a, "--resume=") || strings.HasPrefix(a, "--approve=") {
			continue
		}
		parts = append(parts, a)
	}
	parts = append(parts, "--resume="+runID, "--approve="+gate)
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$&|;<>(){}[]*?") {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("(unencodable: %v)", err)
	}
	return string(b)
}

func indentJSON(v any) string {
	b, err := json.MarshalIndent(v, "  ", "  ")
	if err != nil {
		return fmt.Sprintf("  (unencodable: %v)", err)
	}
	return string(b)
}
