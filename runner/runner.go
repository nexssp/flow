package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"

	"github.com/nexssp/flow/core"
)

// Config bundles all compiler tables, capability resolvers, and
// materializers for a run. It is passed explicitly to prevent mutable
// package-level state.
type Config struct {
	Resolver      core.CapabilityResolver
	Directives    *core.DirectiveTable
	Modifiers     *core.ModifierTable
	Operators     *core.OperatorTable
	Primaries     *core.PrimaryExtensionTable
	Materializers []core.Materializer
	CompileOpts   []core.CompileOption

	// Hooks are applied to every resolver action and to the top-level
	// program before execution. Callers that only want to see the
	// result set this to nil.
	Hooks []action.AnyHook

	// EventSink, when non-nil, is wrapped by observe.Hook and appended
	// to Hooks. Both paths end up on the execution-local resolver's
	// actions; use either, or both.
	EventSink observe.Sink

	// MeasureAllocs wraps the compile and run phases of Execute with
	// MemStats deltas so callers can attribute allocations to the
	// compiler versus the runtime. ReadMemStats triggers a brief
	// stop-the-world pause; leave false on production hot paths. This
	// mirrors the same precedent as CompileDuration and RunDuration:
	// Execute already reports per-phase diagnostics, and this is the
	// allocation counterpart.
	MeasureAllocs bool
}

// Opts controls execution options for a single runner invocation.
type Opts struct {
	Verbosity int
	Info      bool
	Asserts   []string
	Stdout    io.Writer
	Stderr    io.Writer
}

// Run executes a .nflow file from disk.
func Run(ctx context.Context, cfg Config, path string, payload map[string]any, opts Opts) int {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "❌ read: %v\n", err)
		return 1
	}

	return RunSource(ctx, cfg, string(src), path, payload, opts)
}

// RunSource executes workflow DSL directly from in-memory text.
//
// It is a thin wrapper over Execute: it handles --info, installs the
// observer, prints the result, and evaluates @assert: directives.
// Every other piece of orchestration lives in Execute so the same
// pipeline runs across cli, selftest, and embedded binaries.
func RunSource(ctx context.Context, cfg Config, src, name string, payload map[string]any, opts Opts) int {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if opts.Info {
		clean, meta, err := core.Preprocess(ctx, cfg.Directives, src, name)
		if err != nil {
			fmt.Fprintf(stderr, "❌ preprocess: %v\n", err)
			return 1
		}
		ast, parseErr := core.NewParserWithPrimaries(ctx, cfg.Operators, cfg.Primaries, clean).Parse()
		if parseErr != nil {
			fmt.Fprintf(stderr, "❌ parse: %v\n", parseErr)
			return 1
		}
		return PrintInfo(stdout, name, src, meta, ast)
	}

	observer := NewObserver(stdout, opts.Verbosity)
	cfg.Hooks = append(cfg.Hooks, observer.Hook())

	ex, err := Execute(ctx, cfg, src, name, payload)
	total := ex.CompileDuration + ex.RunDuration

	if err != nil {
		fmt.Fprintf(stdout, "✗ %v\n", err)
		if opts.Verbosity >= 1 {
			observer.PrintSummary(stdout)
		}
		return 1
	}

	if opts.Verbosity >= 1 {
		fmt.Fprintf(stdout, "✓ %v\n", ex.Output)
		fmt.Fprintf(stdout, "  compile: %s\n", FormatDuration(ex.CompileDuration))
		fmt.Fprintf(stdout, "  run:     %s\n", FormatDuration(ex.RunDuration))
		observer.PrintSummary(stdout)
	} else {
		fmt.Fprintln(stdout, formatOutput(ex.Output))
	}

	asserts := collectAsserts(ex.Meta, opts.Asserts)
	return RunAssertions(
		stdout,
		ex.Output,
		total.Milliseconds(),
		observer.ActionNames(),
		asserts,
		opts.Verbosity,
	)
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

func formatOutput(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}
