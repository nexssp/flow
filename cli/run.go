package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/runner"
)

const harnessEnv = "NFLOW_HARNESS"

func RunWithBundles(args []string, bundles []core.Bundle) int {
	return RunWithBundlesForRequirements(args, bundles, nil)
}

// RunWithBundlesForRequirements injects bundles linked to their original
// @require targets. This preserves bundle IDs when an explicit package variant
// has a different import-path suffix.
func RunWithBundlesForRequirements(args []string, bundles []core.Bundle, targets []string) int {
	inv, err := newInvocation(bundles, targets)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runCLIWithSignals(context.Background(), inv, args, registerProcessSignals)
}

// RunWithBundleFactoriesForRequirements runs generated CLI code in one host
// scope and adopts each returned bundle before the next factory runs.
func RunWithBundleFactoriesForRequirements(args, targets []string, construct func(func(core.Bundle) error) error) int {
	return withSignalContext(context.Background(), registerProcessSignals, func(ctx context.Context) int {
		return runWithBundleFactories(ctx, targets, construct, func(inv *invocation, runCtx context.Context) int {
			return runCLI(runCtx, inv, args)
		})
	})
}

func dedupeBundleIDs(bundles []core.Bundle) []core.Bundle {
	seen := make(map[string]int, len(bundles))
	out := make([]core.Bundle, 0, len(bundles))
	for i := range bundles {
		b := bundles[i]
		if idx, ok := seen[b.ID]; ok {
			out[idx] = b
			continue
		}
		seen[b.ID] = len(out)
		out = append(out, b)
	}
	return out
}

func withHarness(ctx context.Context, inv *invocation, subcommand, flowPath string, args []string, inProcess func(context.Context, *invocation, []string) int) int {
	if flowPath == "" || isInlineSource(flowPath) {
		return inProcess(ctx, inv, args)
	}
	reqs, err := inv.sourceRequiresFromFile(ctx, flowPath)
	if err != nil {
		return fatalf("%v", err)
	}
	return withHarnessRequirements(ctx, inv, subcommand, flowPath, reqs, args, inProcess)
}

func withHarnessRequirements(ctx context.Context, inv *invocation, subcommand, flowPath string, reqs []require.Requirement, args []string, inProcess func(context.Context, *invocation, []string) int) int {
	if len(reqs) == 0 || os.Getenv(harnessEnv) != "" {
		return inProcess(ctx, inv, args)
	}
	if !requirementsNeedExternalHarness(inv, reqs) {
		return inProcess(ctx, inv, args)
	}

	bin, err := EnsureHarnessContext(ctx, reqs, flowPath)
	if err != nil {
		return fatalf("%v", err)
	}
	return ExecHarnessContext(ctx, bin, append([]string{subcommand}, args...))
}

func requirementsNeedExternalHarness(inv *invocation, reqs []require.Requirement) bool {
	for i := range reqs {
		r := &reqs[i]
		if inv != nil && inv.isAlreadyInjected(*r) {
			continue
		}
		targetID := require.NormalizeID(r.Import)
		if _, ok := core.Lookup(targetID); ok {
			continue
		}
		if _, ok := core.Lookup(r.Import); ok {
			continue
		}
		return true
	}
	return false
}

func runFlow(args []string) int {
	inv, err := newInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(context.Background(), inv, func(ctx context.Context) int {
		return runFlowInInvocation(ctx, inv, args)
	})
}

func runFlowInInvocation(ctx context.Context, inv *invocation, args []string) int {
	target, _, _ := splitArgs(args)
	return withHarness(ctx, inv, "run", target, args, runFlowInProcess)
}

func runFlowInProcess(ctx context.Context, inv *invocation, args []string) int {
	target, _, _ := splitArgs(args)
	if target == "" {
		return fatalf("usage: nflow run <file.nflow | inline_dsl> [json_payload] [flags]")
	}

	var (
		src  string
		name string
	)

	if isInlineSource(target) {
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

	return runSourceInInvocation(ctx, inv, src, name, args)
}

func isInlineSource(target string) bool {
	if _, err := os.Stat(target); err == nil {
		return false
	}
	return strings.Contains(target, "->") ||
		strings.Contains(target, "|") ||
		strings.Contains(target, "@") ||
		strings.Contains(target, "{") ||
		strings.Contains(target, "const") ||
		strings.Contains(target, "noop")
}

func RunEmbeddedWithBundles(ctx context.Context, source string, args []string, bundles []core.Bundle) int {
	return RunEmbeddedWithBundlesForRequirements(ctx, source, args, bundles, nil)
}

// RunEmbeddedWithBundlesForRequirements is the embedded-flow counterpart of
// RunWithBundlesForRequirements.
func RunEmbeddedWithBundlesForRequirements(ctx context.Context, source string, args []string, bundles []core.Bundle, targets []string) int {
	inv, err := newInvocation(bundles, targets)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		return runEmbeddedInInvocation(runCtx, inv, source, args)
	})
}

// RunEmbeddedWithBundleFactoriesForRequirements adopts each generated bundle
// as soon as its factory returns, before later factories or Flow dispatch run.
func RunEmbeddedWithBundleFactoriesForRequirements(ctx context.Context, source string, args, targets []string, construct func(func(core.Bundle) error) error) int {
	return runWithBundleFactories(ctx, targets, construct, func(inv *invocation, runCtx context.Context) int {
		return runEmbeddedInInvocation(runCtx, inv, source, args)
	})
}

func RunEmbedded(ctx context.Context, source string, args []string) int {
	inv, err := newInvocation(nil, nil)
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

	// Suppress the banner when the caller asked for machine-readable
	// output or for the help text. Both mean "no interactive session
	// is happening".
	return runSourceInInvocation(ctx, inv, source, "<embedded>", args)
}

// runEmbeddedInfo renders the embedded flow's pipeline shape without
// printing the version banner. It shares the preprocess → parse path
// with the info branch of runSourceInProcess; the only difference is
// banner suppression, which the caller owns.
func runEmbeddedInfo(ctx context.Context, inv *invocation, source string, args []string) int {
	wantJSON := hasFlag(args, "--json")

	directives, err := inv.directiveTable()
	if err != nil {
		return fatalf("directives: %v", err)
	}
	clean, meta, err := core.Preprocess(ctx, directives, source, "<embedded>")
	if err != nil {
		return fatalf("preprocess: %v", err)
	}

	cfg, err := inv.buildConfig(require.FromMeta(meta))
	if err != nil {
		return fatalf("config: %v", err)
	}

	ast, err := core.NewParserWithFileOffset(
		ctx, cfg.Operators, cfg.PrimariesFor(meta), clean, "<embedded>", 0,
	).Parse()
	if err != nil {
		return fatalf("parse: %v", err)
	}

	if wantJSON {
		if err := runner.PrintInfoJSON(os.Stdout, "<embedded>", meta, ast); err != nil {
			return fatalf("encode: %v", err)
		}
		return 0
	}
	return runner.PrintInfo(os.Stderr, "<embedded>", source, meta, ast)
}

// printEmbeddedHelp writes usage for a packaged flow binary. The
// description comes from the flow's @description directive, so a user
// who receives a binary they did not build can learn what it does.
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
	fmt.Fprintln(w, "  -v | -vv | -vvv  Increase verbosity")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, `  <binary> '{"user_id": 42}'`)
	fmt.Fprintln(w, `  <binary> --assert='result.ok == true'`)
}

// embeddedDescription extracts the @description text from a source
// string without running the full compiler. Cheap; called only for
// `help`.
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
	inv, err := newInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		target, _, _ := splitArgs(args)
		if target == "" {
			fmt.Fprintln(os.Stderr, "usage: nflow <file.nflow> [json] [flags]")
			return 2
		}
		src, err := readSourceFile(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
		return runSourceInInvocation(runCtx, inv, string(src), target, args)
	})
}

func runSourceInProcess(ctx context.Context, src, name string, args []string) int {
	inv, err := newInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		return runSourceInInvocation(runCtx, inv, src, name, args)
	})
}

func runSourceInInvocation(ctx context.Context, inv *invocation, src, name string, args []string) int {
	payload, flags := splitPayloadAndFlags(args)

	payload = readPipedStdin(payload)

	directives, err := inv.directiveTable()
	if err != nil {
		return fatalf("directives: %v", err)
	}
	clean, meta, err := core.Preprocess(ctx, directives, src, name)
	if err != nil {
		return fatalf("preprocess: %v", err)
	}
	reqs := require.FromMeta(meta)

	cfg, err := inv.buildConfig(reqs)
	if err != nil {
		return fatalf("config: %v", err)
	}

	useColorErr := colorEnabled(os.Stderr)
	useColorOut := colorEnabled(os.Stdout)
	isInfo := hasFlag(flags, "-info") || hasFlag(flags, "--info")
	wantJSON := hasFlag(flags, "--json")
	isTerm := isTerminal(os.Stdout)

	if isInfo {
		primaries := cfg.PrimariesFor(meta)
		ast, parseErr := core.NewParserWithFileOffset(
			ctx, cfg.Operators, primaries, clean, name, 0,
		).Parse()
		if parseErr != nil {
			return fatalf("parse: %v", parseErr)
		}

		if wantJSON {
			if err := runner.PrintInfoJSON(os.Stdout, name, meta, ast); err != nil {
				return fatalf("encode: %v", err)
			}
			return 0
		}
		return runner.PrintInfo(os.Stderr, name, src, meta, ast)
	}

	verbosity := countVerbosity(flags)
	observer := runner.NewObserver(os.Stderr, verbosity)
	cfg.Hooks = append(cfg.Hooks, observer.Hook())

	ex, execErr := runner.Execute(ctx, cfg, src, name, payload)
	total := ex.CompileDuration + ex.RunDuration

	if execErr != nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", paint("✗ Pipeline Execution Failed", ansiRed, useColorErr))
		fmt.Fprintf(os.Stderr, "  Error: %v\n", execErr)
		if verbosity >= 1 {
			observer.PrintSummary(os.Stderr)
		}
		return 1
	}

	if verbosity >= 1 {
		fmt.Fprintf(os.Stderr, "✓ Output produced\n")
		fmt.Fprintf(os.Stderr, "  compile: %s\n", runner.FormatDuration(ex.CompileDuration))
		fmt.Fprintf(os.Stderr, "  run:     %s\n", runner.FormatDuration(ex.RunDuration))
		observer.PrintSummary(os.Stderr)
	}

	if wantJSON || !isTerm {
		emitCleanOutput(os.Stdout, ex.Output)
	} else {
		renderPrettyOutput(os.Stdout, ex.Output, useColorOut)
	}

	asserts := collectAsserts(ex.Meta, flagsToAsserts(flags))
	return runner.RunAssertions(
		os.Stderr,
		ex.Output,
		total.Milliseconds(),
		observer.ActionNames(),
		asserts,
		verbosity,
	)
}

func readPipedStdin(current map[string]any) map[string]any {
	stat, err := os.Stdin.Stat()
	if err != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
		return current
	}

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return current
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return current
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		for k, v := range parsed {
			if _, exists := current[k]; !exists {
				current[k] = v
			}
		}
		return current
	}

	if len(current) == 0 {
		var anyVal any
		if err := json.Unmarshal([]byte(trimmed), &anyVal); err == nil {
			current["input"] = anyVal
		} else {
			current["input"] = trimmed
		}
	}
	return current
}

func emitCleanOutput(w io.Writer, output any) {
	if output == nil {
		return
	}
	if s, ok := output.(string); ok {
		fmt.Fprintln(w, s)
		return
	}
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
		fmt.Fprint(w, formatOutputPretty(output))
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
		fmt.Fprint(w, formatOutputPretty(output))
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
	} else {
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
}

func formatOutputPretty(value any) string {
	if s, ok := value.(string); ok {
		return s + "\n"
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v\n", value)
	}
	return string(encoded) + "\n"
}

func collectAsserts(meta map[string]any, extra []string) []string {
	var out []string
	seen := make(map[string]bool)

	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	if list, ok := meta["asserts"].([]string); ok {
		for _, assertion := range list {
			add(assertion)
		}
	}
	for _, assertion := range extra {
		add(assertion)
	}
	return out
}

func splitArgs(args []string) (target string, payload map[string]any, flags []string) {
	payload = map[string]any{}
	flags = make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "{"):
			if err := json.Unmarshal([]byte(a), &payload); err != nil {
				fmt.Fprintf(os.Stderr, "invalid JSON payload ignored: %v\n", err)
			}
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
		case target == "":
			target = a
		default:
			flags = append(flags, a)
		}
	}
	return
}

func splitPayloadAndFlags(args []string) (payload map[string]any, flags []string) {
	_, payload, flags = splitArgs(args)
	return payload, flags
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
