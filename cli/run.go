package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/builder"
	"github.com/nexssp/flow/compiler"
	flowconsole "github.com/nexssp/flow/extensions/console"
	"github.com/nexssp/flow/extensions/require"
	testext "github.com/nexssp/flow/extensions/test"
	"github.com/nexssp/flow/internal/terminal"
	"github.com/nexssp/flow/runner"
)

const harnessEnv = builder.HarnessEnv

const (
	preserveStdinFlag  = "--preserve-stdin"
	suppressOutputFlag = "--suppress-output"
)

// defaultStdinWait bounds how long `run` waits for piped stdin to deliver its
// first byte before proceeding without stdin input. Once a byte has arrived,
// the read continues to EOF without a deadline, so live producers are never
// cut short. The bound prevents `nflow run` from blocking forever in
// environments where stdin is an open pipe that never delivers data and never
// closes (CI runners, containers, editors). NFLOW_STDIN_WAIT overrides it.
const defaultStdinWait = 250 * time.Millisecond

// stdinWaitEnvName is the environment knob for the piped-stdin first-byte
// bound: a non-negative Go duration, where 0s restores the legacy unbounded
// wait.
const stdinWaitEnvName = "NFLOW_STDIN_WAIT"

func RunWithBundles(args []string, bundles []compiler.Bundle) int {
	return RunWithBundlesForRequirements(args, bundles, nil)
}

func RunWithBundlesForRequirements(args []string, bundles []compiler.Bundle, targets []string) int {
	inv, err := newNativeInvocation(bundles, targets)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runCLIWithSignals(context.Background(), inv, args, registerProcessSignals)
}

func RunWithBundleFactoriesForRequirements(args, targets []string, construct func(func(compiler.Bundle) error) error) int {
	return withSignalContext(context.Background(), registerProcessSignals, func(ctx context.Context) int {
		return runWithBundleFactories(ctx, targets, construct, func(inv *invocation, runCtx context.Context) int {
			withNativeDefaults(inv)
			return runCLI(runCtx, inv, args)
		})
	})
}

func RunWithBundleFactoriesAndNative(
	args, targets []string,
	nativeFactory func() []compiler.Bundle,
	construct func(func(compiler.Bundle) error) error,
) int {
	return withSignalContext(context.Background(), registerProcessSignals, func(ctx context.Context) int {
		return runWithBundleFactories(ctx, targets, construct, func(inv *invocation, runCtx context.Context) int {
			inv.nativeFactory = nativeFactory
			return runCLI(runCtx, inv, args)
		})
	})
}

func withHarness(ctx context.Context, inv *invocation, subcommand, flowPath string, args []string, inProcess func(context.Context, *invocation, []string) int) int {
	if flowPath == "" || isInlineSource(ctx, flowPath) {
		return inProcess(ctx, inv, args)
	}
	reqs, err := inv.requirementsForHarness(ctx, flowPath)
	if err != nil {
		source, readErr := readSourceFile(flowPath)
		if readErr != nil {
			return fatalf("%v", err)
		}
		return fatalSourceError("preprocess/config", err, string(source), flowPath)
	}
	return withHarnessRequirements(ctx, inv, subcommand, flowPath, reqs, args, inProcess)
}

func withHarnessRequirements(ctx context.Context, inv *invocation, subcommand, flowPath string, reqs []require.Requirement, args []string, inProcess func(context.Context, *invocation, []string) int) int {
	// The registration check — not the NFLOW_HARNESS marker — decides the
	// dispatch: a plain `nflow` engine exec'd from inside a harness child
	// inherits NFLOW_HARNESS=1, but it does NOT carry the flow's external
	// modules, so it must prepare its own harness for them instead of
	// failing in-process with "bundle ... is not registered in this binary".
	// In a real harness child the requirements resolve as injected/registered
	// and the run stays in-process (no recursion).
	if len(reqs) == 0 || !requirementsNeedExternalHarness(inv, reqs) {
		return inProcess(ctx, inv, args)
	}

	thinSelected := thinHarnessArgsSupported(subcommand, flowPath, args) && terminal.IsTerminal(os.Stdin) && !terminal.IsTerminal(os.Stdout)
	ensureHarness := builder.EnsureHarnessCLIContext
	if thinSelected {
		ensureHarness = builder.EnsureThinHarnessContext
	}
	bin, err := ensureHarness(ctx, reqs, flowPath)
	if err != nil {
		return fatalf("%v", err)
	}
	commandArgs := append([]string{subcommand}, args...)
	return runHarnessWithFallback(ctx, bin, commandArgs, thinSelected, reqs, flowPath,
		builder.EnsureHarnessCLIContext, builder.ExecHarnessContext)
}

func runHarnessWithFallback(
	ctx context.Context,
	bin string,
	commandArgs []string,
	allowFallback bool,
	reqs []require.Requirement,
	flowPath string,
	ensureCLI func(context.Context, []require.Requirement, ...string) (string, error),
	execute func(context.Context, string, []string) int,
) int {
	code := execute(ctx, bin, commandArgs)
	if !allowFallback || code != builder.HarnessFallbackExitCode {
		return code
	}

	legacyBin, err := ensureCLI(ctx, reqs, flowPath)
	if err != nil {
		return fatalf("%v", err)
	}
	return execute(ctx, legacyBin, commandArgs)
}

// thinHarnessArgsSupported is deliberately stricter than the CLI parser: the
// generated runner only preserves a plain file run, one object payload, and
// --json. Anything else stays on the CLI harness rather than approximating
// flags or input behavior.
func thinHarnessArgsSupported(subcommand, flowPath string, args []string) bool {
	if subcommand != "run" || len(args) == 0 || args[0] != flowPath {
		return false
	}

	payloadSeen := false
	for _, arg := range args[1:] {
		if arg == "--json" {
			continue
		}
		if !strings.HasPrefix(arg, "{") || payloadSeen {
			return false
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(arg), &payload); err != nil {
			return false
		}
		payloadSeen = true
	}
	return true
}

func requirementsNeedExternalHarness(inv *invocation, reqs []require.Requirement) bool {
	for i := range reqs {
		r := &reqs[i]
		if inv != nil && inv.isAlreadyInjected(*r) {
			continue
		}
		targetID := require.NormalizeID(r.Import)
		if _, ok := compiler.Lookup(targetID); ok {
			continue
		}
		if _, ok := compiler.Lookup(r.Import); ok {
			continue
		}
		return true
	}
	return false
}

func runFlowInInvocation(ctx context.Context, inv *invocation, args []string) int {
	testMode := slices.Contains(args, testModeFlag)
	if testMode {
		previousModeBundles := inv.modeBundles
		inv.modeBundles = append(append([]compiler.Bundle(nil), previousModeBundles...), testext.Bundle(nil))
		defer func() { inv.modeBundles = previousModeBundles }()
	}

	target, _, _, err := splitArgs(args)
	if err != nil {
		return fatalf("%v", err)
	}
	return withHarness(ctx, inv, "run", target, args, func(runCtx context.Context, runInv *invocation, runArgs []string) int {
		if testMode {
			runArgs = withoutTestModeFlag(runArgs)
		}
		return runFlowInProcess(runCtx, runInv, runArgs)
	})
}

func fatalSourceError(stage string, err error, source, name string) int {
	if _, ok := compiler.PositionOf(err); !ok {
		return fatalf("%s: %v", stage, err)
	}
	detail := runner.FormatErrorDetail(err, source, name, colorEnabled(os.Stderr))
	fmt.Fprintf(os.Stderr, "❌ %s: %s\n", stage, detail)
	return 2
}

func withoutTestModeFlag(args []string) []string {
	if !slices.Contains(args, testModeFlag) {
		return args
	}
	filtered := make([]string, 0, len(args)-1)
	for _, arg := range args {
		if arg != testModeFlag {
			filtered = append(filtered, arg)
		}
	}
	return filtered
}

func runFlowInProcess(ctx context.Context, inv *invocation, args []string) int {
	target, payload, flags, err := splitArgs(args)
	if err != nil {
		return fatalf("%v", err)
	}
	if target == "" {
		return fatalf("usage: nflow run <file.nflow | inline_dsl> [json_payload] [flags]")
	}

	var (
		src  string
		name string
	)

	if isInlineSource(ctx, target) {
		src = target
		name = "<inline>"
	} else {
		data, err := readSourceFile(target)
		if err != nil {
			return fatalf("%v", err)
		}
		src = string(data)
		name = target
	}

	return runSourceInInvocation(ctx, inv, src, name, payload, flags)
}

func isInlineSource(ctx context.Context, target string) bool {
	if _, err := os.Stat(target); err == nil {
		return false
	}
	source := strings.TrimSpace(target)
	if source == "" {
		return false
	}

	if expr, err := compiler.NewParser(ctx, compiler.NewOperatorTable(), source).Parse(); err == nil {
		switch node := expr.(type) {
		case *compiler.LiteralExpr, *compiler.ValueExpr:
			return true
		case *compiler.Atom:
			if compiler.IsReservedActionName(node.Name) ||
				strings.HasPrefix(node.Name, "runtime.") ||
				strings.HasPrefix(node.Name, "pipeline.") {
				return true
			}
		}
	}

	if strings.HasSuffix(strings.ToLower(source), ".nflow") || strings.ContainsAny(source, `/\`) {
		return false
	}

	if strings.Contains(source, "->") ||
		strings.ContainsAny(source, "|&{}[]()@?:\"'`") {
		return true
	}

	// Missing .nflow files and path-like targets must remain file requests;
	// never infer inline source from words embedded in their names.
	return false
}

func RunEmbeddedWithBundles(ctx context.Context, source string, args []string, bundles []compiler.Bundle) int {
	return RunEmbeddedWithBundlesForRequirements(ctx, source, args, bundles, nil)
}

func RunEmbeddedWithBundlesForRequirements(ctx context.Context, source string, args []string, bundles []compiler.Bundle, targets []string) int {
	inv, err := newNativeInvocation(bundles, targets)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		return runEmbeddedInInvocation(runCtx, inv, source, args)
	})
}

func RunEmbeddedWithBundleFactoriesForRequirements(ctx context.Context, source string, args, targets []string, construct func(func(compiler.Bundle) error) error) int {
	return runWithBundleFactories(ctx, targets, construct, func(inv *invocation, runCtx context.Context) int {
		return runEmbeddedInInvocation(runCtx, inv, source, args)
	})
}

func RunEmbedded(ctx context.Context, source string, args []string) int {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		return runEmbeddedInInvocation(runCtx, inv, source, args)
	})
}

func runEmbeddedInInvocation(ctx context.Context, inv *invocation, source string, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version":
			Print(os.Stdout)
			return 0
		case "help", "--help", "-h":
			printEmbeddedHelp(os.Stdout, source)
			return 0
		case "info":
			return runEmbeddedInfo(ctx, inv, source, args[1:])
		}
	}

	payload, flags, err := splitPayloadAndFlags(args)
	if err != nil {
		return fatalf("%v", err)
	}
	if !hasFlag(flags, "--json") {
		v, _, b := resolvedBuildInfo()
		fmt.Fprintf(os.Stderr, "nexssflow %s (built %s)\n", v, b)
	}

	return runSourceInInvocation(ctx, inv, source, "<embedded>", payload, flags)
}

func runEmbeddedInfo(ctx context.Context, inv *invocation, source string, args []string) int {
	wantJSON := hasFlag(args, "--json")

	clean, meta, cfg, err := inv.prepareRequiredSource(ctx, source, "<embedded>")
	if err != nil {
		return fatalSourceError("preprocess/config", err, source, "<embedded>")
	}

	ast, err := compiler.NewParserWithFileOffset(
		ctx, cfg.Operators, cfg.PrimariesFor(meta), clean, "<embedded>", 0,
	).Parse()
	if err != nil {
		return fatalSourceError("parse", err, source, "<embedded>")
	}

	if wantJSON {
		if err := runner.PrintInfoJSON(os.Stdout, "<embedded>", meta, ast); err != nil {
			return fatalf("encode: %v", err)
		}
		return 0
	}
	return runner.PrintInfo(os.Stderr, "<embedded>", source, meta, ast)
}

func printEmbeddedHelp(w io.Writer, source string) {
	fmt.Fprintln(w, "A Nexss Flow executable.")
	fmt.Fprintln(w)

	if desc := embeddedDescription(source); desc != "" {
		fmt.Fprintf(w, "  %s\n\n", desc)
	}

	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  <binary> [json_payload] [flags]")
	fmt.Fprintln(w, "  <binary> version")
	fmt.Fprintln(w, "  <binary> info")
	fmt.Fprintln(w, "  <binary> help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --json           Emit clean JSON to stdout")
	fmt.Fprintln(w, "  --info           Describe the pipeline instead of running it")
	fmt.Fprintln(w, "  --assert=EXPR    Evaluate EXPR after the run; non-zero exit on failure")
	fmt.Fprintln(w, "  --preserve-stdin Do not read piped stdin as workflow input")
	fmt.Fprintln(w, "  --suppress-output Suppress the runner's final result on stdout")
	fmt.Fprintln(w, "  --journal-dir DIR   Persist durable step results in DIR")
	fmt.Fprintln(w, "  --execution-id ID   Resume the same run (requires --journal-dir)")
	fmt.Fprintln(w, "  --approval TOKEN    Approve named @approval gates (or all)")
	fmt.Fprintln(w, "  -v | -vv | -vvv  Increase verbosity")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, `  <binary> '{"user_id": 42}'`)
	fmt.Fprintln(w, `  <binary> --assert='result.ok == true'`)
}

func embeddedDescription(source string) string {
	for line := range strings.SplitSeq(source, "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "@description")
		if !ok {
			continue
		}
		return strings.Trim(strings.TrimSpace(rest), `"'`)
	}
	return ""
}

func RunPath(ctx context.Context, args []string) int {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		target, payload, flags, err := splitArgs(args)
		if err != nil {
			return fatalf("%v", err)
		}
		if target == "" {
			fmt.Fprintln(os.Stderr, "usage: nflow <file.nflow> [json] [flags]")
			return 2
		}
		src, err := readSourceFile(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
		return runSourceInInvocation(runCtx, inv, string(src), target, payload, flags)
	})
}

func runSourceInProcess(ctx context.Context, src, name string, args []string) int {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		payload, flags, err := splitPositionalsAndFlags(args)
		if err != nil {
			return fatalf("%v", err)
		}
		return runSourceInInvocation(runCtx, inv, src, name, payload, flags)
	})
}

func runSourceInInvocation(ctx context.Context, inv *invocation, src, name string, payload map[string]any, flags []string) int {
	suppressOutput := hasFlag(flags, suppressOutputFlag)
	flags = withoutFlag(flags, suppressOutputFlag)
	durability, flags, err := runner.ParseDurabilityOptions(flags)
	if err != nil {
		return fatalf("run options: %v", err)
	}
	approvalToken, flags, err := runner.ParseApprovalOption(flags)
	if err != nil {
		return fatalf("run options: %v", err)
	}
	if approvalToken != "" {
		ctx = xctx.WithApprovalToken(ctx, approvalToken)
	}

	clean, meta, cfg, err := inv.prepareRequiredSource(ctx, src, name)
	if err != nil {
		return fatalSourceError("preprocess/config", err, src, name)
	}

	payload, flags, err = readRunInput(payload, clean, flags)
	if err != nil {
		return fatalf("%v", err)
	}

	cfg.CompileOpts = append(cfg.CompileOpts, compiler.WithCLIArgs(flags))

	if hasFlag(flags, "-info") || hasFlag(flags, "--info") {
		return printSourceInfo(ctx, cfg, src, name, clean, meta, hasFlag(flags, "--json"))
	}
	return runPreparedSource(ctx, cfg, src, name, payload, flags, durability, suppressOutput)
}

func printSourceInfo(ctx context.Context, cfg runner.Config, src, name, clean string, meta map[string]any, wantJSON bool) int {
	ast, err := compiler.NewParserWithFileOffset(
		ctx, cfg.Operators, cfg.PrimariesFor(meta), clean, name, 0,
	).Parse()
	if err != nil {
		return fatalSourceError("parse", err, src, name)
	}
	if wantJSON {
		if printErr := runner.PrintInfoJSON(os.Stdout, name, meta, ast); printErr != nil {
			return fatalf("encode: %v", printErr)
		}
		return 0
	}
	return runner.PrintInfo(os.Stderr, name, src, meta, ast)
}

func runPreparedSource(ctx context.Context, cfg runner.Config, src, name string, payload map[string]any, flags []string, durability runner.DurabilityOptions, suppressOutput bool) int {
	useColorErr := colorEnabled(os.Stderr)
	useColorOut := colorEnabled(os.Stdout)
	wantJSON := hasFlag(flags, "--json")
	isTerm := terminal.IsTerminal(os.Stdout)

	ctx, cfg, closeJournal, err := configureRunDurability(ctx, cfg, durability)
	if err != nil {
		return fatalf("%v", err)
	}
	if closeJournal != nil {
		defer closeJournal()
	}

	verbosity := countVerbosity(flags)
	observer := runner.NewObserver(os.Stderr, verbosity)
	cfg.Hooks = append(cfg.Hooks, observer.Hook())

	ctx = flowconsole.WithStderrWriter(ctx, os.Stderr)
	ex, execErr := runner.Execute(ctx, cfg, src, name, payload)
	total := ex.CompileDuration + ex.RunDuration
	if cfg.Journal != nil && ex.ExecutionID != "" {
		fmt.Fprintf(os.Stderr, "Execution ID: %s\n", ex.ExecutionID)
	}

	if execErr != nil {
		return reportPreparedExecutionFailure(ex, execErr, src, name, durability, useColorErr, verbosity, observer)
	}

	if verbosity >= 1 {
		fmt.Fprintf(os.Stderr, "✓ Output produced\n")
		fmt.Fprintf(os.Stderr, "  compile: %s\n", runner.FormatDuration(ex.CompileDuration))
		fmt.Fprintf(os.Stderr, "  run:     %s\n", runner.FormatDuration(ex.RunDuration))
		observer.PrintSummary(os.Stderr)
	}

	emitPreparedOutput(ex.Output, suppressOutput || ex.SuppressOutput, wantJSON, isTerm, useColorOut)

	asserts, positions := runner.CollectAsserts(ex.Meta, flagsToAsserts(flags))
	return runner.RunAssertionsContextWithPositionsAndActionHistory(
		ctx,
		os.Stderr,
		ex.Output,
		total.Milliseconds(),
		observer.ActionNames(),
		observer.ActionNamesComplete(),
		asserts,
		positions,
		verbosity,
		compiler.SymbolsFromMeta(ex.Meta),
	)
}

func configureRunDurability(ctx context.Context, cfg runner.Config, durability runner.DurabilityOptions) (context.Context, runner.Config, func(), error) {
	var journal *runner.FileJournal
	if durability.JournalDir != "" {
		if cfg.Journal != nil {
			return ctx, cfg, nil, errors.New("run options: --journal-dir cannot be combined with a journal already set in runner.Config")
		}
		var err error
		journal, err = runner.NewFileJournal(durability.JournalDir)
		if err != nil {
			return ctx, cfg, nil, fmt.Errorf("journal: %w", err)
		}
		cfg.Journal = journal
		if cfg.IdempotencyStore == nil {
			cfg.IdempotencyStore = journal
		}
	}
	if durability.ExecutionID != "" && cfg.Journal == nil {
		return ctx, cfg, nil, errors.New("run options: --execution-id requires --journal-dir")
	}
	if durability.ExecutionID != "" {
		ctx = xctx.WithExecutionID(ctx, durability.ExecutionID)
	}
	if journal == nil {
		return ctx, cfg, nil, nil
	}
	closeJournal := func() {
		if err := journal.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "journal close: %v\n", err)
		}
	}
	return ctx, cfg, closeJournal, nil
}

func reportPreparedExecutionFailure(ex runner.Execution, execErr error, src, name string, durability runner.DurabilityOptions, useColorErr bool, verbosity int, observer *runner.Observer) int {
	if ex.Suspension != nil && errors.Is(execErr, dag.ErrSuspended) {
		printApprovalSuspension(os.Stderr, name, durability, ex.Suspension)
		return 3
	}
	if ex.Suspension != nil {
		fmt.Fprintf(os.Stderr, "  Suspension persistence error: %s\n", formatCleanError(execErr, useColorErr))
		fmt.Fprintln(os.Stderr, "  Approval pause was not persisted; configure a suspension-capable journal to resume safely.")
	}
	fmt.Fprintf(os.Stderr, "\n%s\n\n", paint("✗ Pipeline Execution Failed", ansiRed, useColorErr))
	fmt.Fprintf(os.Stderr, "  %s\n\n", runner.FormatErrorDetail(execErr, src, name, useColorErr))
	if verbosity >= 1 {
		if trace := runner.FormatErrorTrace(execErr); trace != "" {
			fmt.Fprintln(os.Stderr, trace)
		}
		fmt.Fprintf(os.Stderr, "  debug: compile finished in %s\n", runner.FormatDuration(ex.CompileDuration))
		if ex.RunDuration == 0 {
			fmt.Fprintln(os.Stderr, "  debug: execution did not start; no runtime action events are expected")
		}
		observer.PrintSummary(os.Stderr)
	}
	if ex.RunDuration == 0 {
		return 2
	}
	return 1
}

func emitPreparedOutput(output any, suppressOutput, wantJSON, isTerm, useColor bool) {
	if suppressOutput {
		return
	}
	if wantJSON {
		emitJSONOutput(os.Stdout, output)
		return
	}
	if !isTerm {
		emitCleanOutput(os.Stdout, output)
		return
	}
	renderPrettyOutput(os.Stdout, output, useColor)
}

func readPipedStdin(current map[string]any) (map[string]any, error) {
	stdin := os.Stdin
	stat, err := stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat stdin: %w", err)
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return current, nil
	}

	wait := stdinWait()
	raw, timedOut, err := readStdinBounded(stdin, wait)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	if timedOut {
		fmt.Fprintf(os.Stderr, "nflow: stdin is open but delivered no input within %s; continuing without it (pipe a JSON payload, or set %s=0s to wait indefinitely)\n", wait, stdinWaitEnvName)
		return current, nil
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return current, nil
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		for k, v := range parsed {
			if _, exists := current[k]; !exists {
				current[k] = v
			}
		}
		return current, nil
	}

	if len(current) == 0 {
		var anyVal any
		if err := json.Unmarshal([]byte(trimmed), &anyVal); err == nil {
			current["input"] = anyVal
		} else {
			current["input"] = trimmed
		}
	}
	return current, nil
}

// stdinWait resolves the first-byte piped-stdin bound. Empty uses the
// default; 0 or a negative duration waits indefinitely; invalid values warn
// and fall back to the default, matching NFLOW_GO_TIMEOUT's convention.
func stdinWait() time.Duration {
	raw := strings.TrimSpace(os.Getenv(stdinWaitEnvName))
	if raw == "" {
		return defaultStdinWait
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nflow: ignoring invalid %s %q (expected a non-negative Go duration such as 2s); using %s\n", stdinWaitEnvName, raw, defaultStdinWait)
		return defaultStdinWait
	}
	return parsed
}

// readStdinBounded reads all of stdin but bounds the wait for the FIRST byte.
// If no byte arrives within wait, it reports timedOut without consuming
// anything. After the first byte the read blocks until EOF exactly as before,
// so producers that are slow but live are never truncated. A bounded read
// requires deadline support; file-backed stdin uses an OS readiness check when
// the file does not support deadlines. wait <= 0 waits indefinitely.
func readStdinBounded(stdin io.Reader, wait time.Duration) (data []byte, timedOut bool, err error) {
	if wait <= 0 {
		data, err = io.ReadAll(stdin)
		return data, false, err
	}
	if file, ok := stdin.(*os.File); ok {
		info, statErr := file.Stat()
		if statErr != nil {
			return nil, false, fmt.Errorf("stat stdin: %w", statErr)
		}
		if info.Mode().IsRegular() {
			data, err = io.ReadAll(stdin)
			return data, false, err
		}
	}
	deadlineReader, ok := stdin.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		return nil, false, errors.New("bounded stdin read requires a reader with read-deadline support")
	}
	deadlineErr := deadlineReader.SetReadDeadline(time.Now().Add(wait))
	if deadlineErr != nil {
		file, isFile := stdin.(*os.File)
		if !isFile {
			return nil, false, fmt.Errorf("set stdin read deadline: %w", deadlineErr)
		}
		ready, waitErr := waitForFileReadable(file, wait)
		if waitErr != nil {
			return nil, false, fmt.Errorf("set stdin read deadline: %w; wait for stdin: %w", deadlineErr, waitErr)
		}
		if !ready {
			return nil, true, nil
		}
	}
	first := make([]byte, 1)
	n, readErr := stdin.Read(first)
	if deadlineErr == nil {
		clearErr := deadlineReader.SetReadDeadline(time.Time{})
		if clearErr != nil {
			return nil, false, fmt.Errorf("clear stdin read deadline: %w", clearErr)
		}
	}
	if n == 0 && errors.Is(readErr, os.ErrDeadlineExceeded) {
		return nil, true, nil
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, os.ErrDeadlineExceeded) {
		return nil, false, readErr
	}
	if n == 0 {
		// Immediate EOF: empty stdin, consumed without a timeout.
		return []byte{}, false, nil
	}
	rest, restErr := io.ReadAll(stdin)
	if restErr != nil {
		return nil, false, restErr
	}
	return append(first[:n], rest...), false, nil
}

func readRunInput(payload map[string]any, source string, flags []string) (input map[string]any, remainingFlags []string, err error) {
	if hasFlag(flags, preserveStdinFlag) {
		return payload, withoutFlag(flags, preserveStdinFlag), nil
	}
	if sourceUsesStdinStream(source) {
		return payload, flags, nil
	}
	input, err = readPipedStdin(payload)
	return input, flags, err
}

func sourceUsesStdinStream(source string) bool {
	lexer := compiler.NewLexer(source)
	for {
		token := lexer.Next()
		if token.Type == compiler.TokEOF {
			return false
		}
		if token.Type == compiler.TokIdent && token.Lit == "io.stdin" {
			return true
		}
	}
}

func withoutFlag(args []string, flag string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != flag {
			filtered = append(filtered, arg)
		}
	}
	return filtered
}

func emitCleanOutput(w io.Writer, output any) {
	if output == nil {
		return
	}
	if s, ok := output.(string); ok {
		fmt.Fprintln(w, s)
		return
	}
	emitJSONOutput(w, output)
}

func emitJSONOutput(w io.Writer, output any) {
	encoded, err := json.Marshal(output)
	if err != nil {
		fmt.Fprintf(w, "%v\n", output)
		return
	}
	fmt.Fprintln(w, string(encoded))
}

func renderPrettyOutput(w *os.File, output any, useColor bool) {
	m, ok := output.(map[string]any)
	if !ok {
		renderColoredJSON(w, output, useColor)
		return
	}

	stdout, hasStdout := m["stdout"].(string)
	stderr, hasStderr := m["stderr"].(string)
	passed, isExec := m["passed"].(bool)
	exitCode, hasExitCode := m["exit_code"].(int)
	if !hasExitCode {
		if exitFloat, ok := m["exit_code"].(float64); ok {
			exitCode = int(exitFloat)
			hasExitCode = true
		}
	}

	if !isExec && !hasStdout && !hasStderr && !hasExitCode {
		renderColoredJSON(w, output, useColor)
		return
	}

	if isExec && passed {
		if hasStdout && strings.TrimSpace(stdout) != "" {
			fmt.Fprintf(w, "%s", stdout)
			if !strings.HasSuffix(stdout, "\n") {
				fmt.Fprintln(w)
			}
		} else {
			fmt.Fprintf(w, "%s\n", paint("▶ EXECUTION SUCCESS (no stdout)", ansiGreen, useColor))
		}
		return
	}

	fmt.Fprintf(w, "\n%s (code %d)\n", paint("▶ EXECUTION FAILED", ansiRed, useColor), exitCode)
	if hasStdout && strings.TrimSpace(stdout) != "" {
		fmt.Fprintf(w, "%s\n", paint("STDOUT:", ansiBold, useColor))
		for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if hasStderr && strings.TrimSpace(stderr) != "" {
		fmt.Fprintf(w, "%s\n", paint("STDERR:", ansiRed, useColor))
		for line := range strings.SplitSeq(strings.TrimSpace(stderr), "\n") {
			fmt.Fprintf(w, "  %s\n", paint(line, ansiRed, useColor))
		}
	}
}

func renderColoredJSON(w io.Writer, value any, useColor bool) {
	if s, ok := value.(string); ok {
		fmt.Fprintln(w, s)
		return
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "%v\n", value)
		return
	}
	for line := range strings.SplitSeq(string(encoded), "\n") {
		fmt.Fprintln(w, colorizeJSONLine(line, useColor))
	}
}

func splitArgs(args []string) (target string, payload map[string]any, flags []string, err error) {
	return splitArgsWithMode(args, true, true)
}

func splitPayloadAndFlags(args []string) (payload map[string]any, flags []string, err error) {
	_, payload, flags, err = splitArgsWithMode(args, false, true)
	return payload, flags, err
}

func splitPositionalsAndFlags(args []string) (payload map[string]any, flags []string, err error) {
	_, payload, flags, err = splitArgsWithMode(args, false, false)
	return payload, flags, err
}

func splitArgsWithMode(args []string, consumeTarget, parseJSONPayload bool) (target string, payload map[string]any, flags []string, err error) {
	payload = map[string]any{}
	flags = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--journal-dir" || a == "--execution-id" || a == "--approval":
			if i+1 < len(args) {
				flags = append(flags, a+"="+args[i+1])
				i++
			} else {
				flags = append(flags, a)
			}
		case parseJSONPayload && strings.HasPrefix(a, "{"):
			if err := json.Unmarshal([]byte(a), &payload); err != nil {
				return "", nil, nil, fmt.Errorf("invalid JSON payload: %w", err)
			}
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
		case consumeTarget && target == "":
			target = a
		default:
			flags = append(flags, a)
		}
	}
	return
}

func countVerbosity(flags []string) int {
	n := 0
	for _, f := range flags {
		if f == "--verbose" {
			n++
			continue
		}
		if strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") {
			vCount := strings.Count(f, "v")
			if vCount > 0 && vCount == len(f)-1 {
				n += vCount
			}
		}
	}
	return n
}

func flagsToAsserts(flags []string) []string {
	var out []string
	for _, f := range flags {
		if after, ok := strings.CutPrefix(f, "--assert="); ok {
			out = append(out, after)
		}
	}
	return out
}

func formatCleanError(err error, useColor bool) string {
	return runner.FormatErrorDetail(err, "", "", useColor)
}
