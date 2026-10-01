package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

const harnessEnv = "NFLOW_HARNESS"

var injectedBundles []core.Bundle

func RunWithBundles(args []string, bundles []core.Bundle) int {
	injectedBundles = append([]core.Bundle(nil), bundles...)
	defer func() { injectedBundles = nil }()
	return Run(args)
}

func buildConfig(reqs []require.Requirement) (runner.Config, error) {
	unresolved := make([]require.Requirement, 0, len(reqs))
	for i := range reqs {
		r := &reqs[i]
		if isAlreadyInjected(*r, injectedBundles) {
			continue
		}
		unresolved = append(unresolved, *r)
	}

	required, err := require.ResolveBundles(unresolved)
	if err != nil {
		return runner.Config{}, err
	}
	bundles := append([]core.Bundle{}, native.Bundles()...)
	bundles = append(bundles, injectedBundles...)
	bundles = append(bundles, required...)
	return runner.BuildConfig(dedupeBundleIDs(bundles))
}

func isAlreadyInjected(r require.Requirement, injected []core.Bundle) bool {
	if len(injected) == 0 {
		return false
	}
	targetID := require.NormalizeID(r.Import)
	for i := range injected {
		b := &injected[i]
		if b.ID == targetID || b.ID == r.Import || b.ID == r.Alias {
			return true
		}
		if r.IsLoose && (b.ID == filepath.Base(r.LocalPath) || b.ID == targetID) {
			return true
		}
	}
	return false
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

func withHarness(subcommand, flowPath string, args []string, inProcess func([]string) int) int {
	if flowPath == "" || isInlineSource(flowPath) {
		return inProcess(args)
	}

	reqs, err := sourceRequiresFromFile(flowPath)
	if err != nil {
		return fatalf("%v", err)
	}

	if len(reqs) == 0 || os.Getenv(harnessEnv) != "" {
		return inProcess(args)
	}

	allInternal := true
	for i := range reqs {
		r := &reqs[i]
		targetID := require.NormalizeID(r.Import)
		if _, ok := core.Lookup(targetID); !ok {
			if _, ok := core.Lookup(r.Import); !ok {
				allInternal = false
				break
			}
		}
	}
	if allInternal {
		return inProcess(args)
	}

	bin, err := EnsureHarness(reqs, flowPath)
	if err != nil {
		return fatalf("%v", err)
	}
	return ExecHarness(bin, append([]string{subcommand}, args...))
}

func runFlow(args []string) int {
	target, _, _ := splitArgs(args)
	return withHarness("run", target, args, runFlowInProcess)
}

func runFlowInProcess(args []string) int {
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
		data, err := os.ReadFile(target)
		if err != nil {
			return fatalf("read: %v", err)
		}
		src = string(data)
		name = target
	}

	return runSourceInProcess(context.Background(), src, name, args)
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
	injectedBundles = append([]core.Bundle(nil), bundles...)
	defer func() { injectedBundles = nil }()
	fmt.Fprintf(os.Stderr, "nexssflow %s %s (built %s)\n", Version, Commit, BuiltAt)
	return runSourceInProcess(ctx, source, "<embedded>", args)
}

func RunEmbedded(ctx context.Context, source string, args []string) int {
	fmt.Fprintf(os.Stderr, "nexssflow %s %s (built %s)\n", Version, Commit, BuiltAt)
	return runSourceInProcess(ctx, source, "<embedded>", args)
}

func RunPath(ctx context.Context, args []string) int {
	target, _, _ := splitArgs(args)
	if target == "" {
		fmt.Fprintln(os.Stderr, "usage: nflow <file.nflow> [json] [flags]")
		return 2
	}
	src, err := os.ReadFile(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		return 1
	}
	return runSourceInProcess(ctx, string(src), target, args)
}

func runSourceInProcess(ctx context.Context, src, name string, args []string) int {
	payload, flags := splitPayloadAndFlags(args)

	payload = readPipedStdin(payload)

	_, meta, err := core.Preprocess(ctx, native.Directives(), src, name)
	if err != nil {
		return fatalf("preprocess: %v", err)
	}
	reqs := require.FromMeta(meta)

	cfg, err := buildConfig(reqs)
	if err != nil {
		return fatalf("config: %v", err)
	}

	useColorErr := colorEnabled(os.Stderr)
	useColorOut := colorEnabled(os.Stdout)
	isInfo := hasFlag(flags, "-info") || hasFlag(flags, "--info")
	wantJSON := hasFlag(flags, "--json")
	isTerm := isTerminal(os.Stdout)

	if isInfo {
		ast, parseErr := core.NewParserWithPrimaries(ctx, cfg.Operators, cfg.Primaries, src).Parse()
		if parseErr != nil {
			return fatalf("parse: %v", parseErr)
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

func sourceRequiresFromFile(path string) ([]require.Requirement, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, meta, err := core.Preprocess(context.Background(),
		native.Directives(), string(src), path)
	if err != nil {
		return nil, err
	}
	return require.FromMeta(meta), nil
}

func SourceRequires(path string) ([]require.Requirement, error) {
	return sourceRequiresFromFile(path)
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
