package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xfile"

	compiler "github.com/nexssp/flow/compiler"
)

// Config bundles all compiler tables, capability resolvers, and
// materializers for a run. It is passed explicitly to prevent mutable
// package-level state.
type Config struct {
	Resolver compiler.CapabilityResolver
	// FileResolver routes typed file references registered by bundles. BuildConfig
	// composes provider bindings and Execute exposes the resolver to actions.
	FileResolver  xfile.Resolver
	Directives    *compiler.DirectiveTable
	Modifiers     *compiler.ModifierTable
	Operators     *compiler.OperatorTable
	Primaries     *compiler.PrimaryExtensionTable
	Materializers []compiler.Materializer
	CompileOpts   []compiler.CompileOption
	// Journal, when non-nil, makes each source-positioned capability call
	// durable through Kernel's action.Durable middleware.
	Journal action.Journal
	// IdempotencyStore replaces Kernel's default in-memory store for actions
	// carrying :idempotent. It may be the same object as Journal when the
	// backend implements both contracts.
	IdempotencyStore action.IdempotencyStore

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

	// ArgSchemas is the merged per-action argument schema published by
	// every mounted bundle. It is exposed so diagnostics (nflow lint
	// today) can validate @{...} capability references without building
	// the action graph. Build-time advisors are the runtime counterpart;
	// this field makes the same checks reachable from a pure parse.
	ArgSchemas map[string][]compiler.ArgFieldSpec
}

// Opts controls execution options for a single runner invocation.
type Opts struct {
	Verbosity int
	// Info describes the source without executing it. Direct RunSource callers
	// receive human-readable info on Stdout; the CLI handles --info separately.
	Info    bool
	Asserts []string
	// ApprovalToken grants the named @approval gates, or all gates when set to
	// "all". The caller is responsible for authenticating the approver.
	ApprovalToken string
	// ExecutionID can be set when a run must resume or replay against the same
	// durable journal. An empty value lets the runner generate a new ID.
	ExecutionID string
	// JournalDir opens a FileJournal for this invocation. When omitted, the
	// caller may still provide a journal directly in Config.
	JournalDir string
	Stdout     io.Writer
	Stderr     io.Writer
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
	if opts.ApprovalToken != "" {
		ctx = xctx.WithApprovalToken(ctx, opts.ApprovalToken)
	}

	if opts.Info {
		return printSourceInfo(ctx, cfg, src, name, stdout, stderr)
	}
	var journal *FileJournal
	if opts.JournalDir != "" {
		if cfg.Journal != nil {
			fmt.Fprintln(stderr, "❌ run: --journal-dir cannot be combined with a journal already set in runner.Config")
			return 2
		}
		var err error
		journal, err = NewFileJournal(opts.JournalDir)
		if err != nil {
			fmt.Fprintf(stderr, "❌ journal: %v\n", err)
			return 2
		}
		cfg.Journal = journal
		if cfg.IdempotencyStore == nil {
			cfg.IdempotencyStore = journal
		}
		defer func() {
			if err := journal.Close(); err != nil {
				fmt.Fprintf(stderr, "❌ journal close: %v\n", err)
			}
		}()
	}
	if opts.ExecutionID != "" && cfg.Journal == nil {
		fmt.Fprintln(stderr, "❌ run: --execution-id requires a durable journal; set --journal-dir or runner.Config.Journal")
		return 2
	}
	if opts.ExecutionID != "" {
		ctx = xctx.WithExecutionID(ctx, opts.ExecutionID)
	}

	observer := NewObserver(stdout, opts.Verbosity)
	cfg.Hooks = append(cfg.Hooks, observer.Hook())

	ex, err := Execute(ctx, cfg, src, name, payload)
	total := ex.CompileDuration + ex.RunDuration
	if cfg.Journal != nil && ex.ExecutionID != "" {
		fmt.Fprintf(stderr, "Execution ID: %s\n", ex.ExecutionID)
	}

	if err != nil {
		if ex.Suspension != nil {
			fmt.Fprintf(stderr, "Approval required: gate %s (role %s): %s\n", ex.Suspension.Gate, ex.Suspension.Role, ex.Suspension.Reason)
			if !ex.Suspension.Persisted {
				fmt.Fprintln(stderr, "Approval pause was not persisted; configure a suspension-capable journal to resume safely.")
			}
			return 3
		}
		useColor := outputColorEnabled(stderr)
		heading := diagnosticPaint("✗ Pipeline Execution Failed", diagnosticANSIRed, useColor)
		fmt.Fprintf(stderr, "\n%s\n\n  %s\n\n", heading, FormatErrorDetail(err, src, name, useColor))
		if opts.Verbosity >= 1 {
			observer.PrintSummary(stderr)
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

	asserts, positions := collectAsserts(ex.Meta, opts.Asserts)
	return RunAssertionsContextWithPositions(
		ctx,
		stderr,
		ex.Output,
		total.Milliseconds(),
		observer.ActionNames(),
		asserts,
		positions,
		opts.Verbosity,
		compiler.SymbolsFromMeta(ex.Meta),
	)
}

func printSourceInfo(ctx context.Context, cfg Config, src, name string, stdout, stderr io.Writer) int {
	clean, meta, err := compiler.Preprocess(ctx, cfg.Directives, src, name)
	if err != nil {
		fmt.Fprintf(stderr, "❌ preprocess: %v\n", err)
		return 1
	}
	ast, parseErr := compiler.NewParserWithFileOffset(
		ctx, cfg.Operators, cfg.PrimariesFor(meta), clean, name, 0,
	).Parse()
	if parseErr != nil {
		fmt.Fprintf(stderr, "❌ parse: %v\n", parseErr)
		return 1
	}
	return PrintInfo(stdout, name, src, meta, ast)
}

func collectAsserts(meta map[string]any, extra []string) ([]string, []compiler.Position) {
	var out []string
	var positions []compiler.Position
	if list, ok := meta["asserts"].([]string); ok {
		sourcePositions, _ := meta["asserts.positions"].([]compiler.Position)
		for i, assertion := range list {
			if assertion == "" {
				continue
			}
			out = append(out, assertion)
			if i < len(sourcePositions) {
				positions = append(positions, sourcePositions[i])
			} else {
				positions = append(positions, compiler.Position{})
			}
		}
	}
	for _, assertion := range extra {
		if assertion == "" {
			continue
		}
		out = append(out, assertion)
		positions = append(positions, compiler.Position{})
	}
	return out, positions
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
