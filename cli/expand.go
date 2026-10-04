package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/runner"
)

// runExpand prints every macro expansion that occurs in a .nflow
// source, in source order. It preprocesses and parses with macros
// active but does not execute the flow. Errors from a failed expansion
// are rendered alongside the substituted body, so the failing token is
// visible without a debugger.
func runExpand(args []string) int {
	if len(args) == 0 {
		return fatalf("usage: nflow expand <file.nflow> [--macro=NAME[,NAME...]]")
	}
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		return fatalf("read: %v", err)
	}
	return withHarness("expand", path, args, runExpandInProcess)
}

func runExpandInProcess(args []string) int {
	if len(args) < 1 {
		return fatalf("usage: nflow expand <file.nflow> [--macro=NAME]")
	}
	path := args[0]

	filter := parseExpandFilter(args[1:])

	src, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read: %v", err)
	}

	reqs, err := sourceRequiresFromFile(path)
	if err != nil {
		return fatalf("requires: %v", err)
	}

	cfg, err := buildConfig(reqs)
	if err != nil {
		return fatalf("config: %v", err)
	}

	exps, err := collectExpansions(cfg, string(src), path)
	if err != nil {
		// Even on parse failure, whatever succeeded before the error is
		// still useful; render it before printing the error.
		renderExpansions(os.Stdout, exps, path, filter)
		return fatalf("%v", err)
	}

	if len(exps) == 0 {
		fmt.Fprintf(os.Stdout, "no macros expanded in %s\n", path)
		return 0
	}

	renderExpansions(os.Stdout, exps, path, filter)
	return 0
}

// collectExpansions runs the same preprocess + parse path the compiler
// uses, with a trace installed, and returns every expansion that
// occurred — top-level first, then each @pipeline body in source order.
//
// Primaries come from cfg.PrimariesFor, the single function the runner
// exposes for building an effective primary table. Reading cfg.Primaries
// directly would drop per-source primaries such as the macro engine,
// and the same diagnostic would appear here as it appeared in lint.
func collectExpansions(cfg runner.Config, src, path string) ([]macros.Expansion, error) {
	trace := &macros.Trace{}

	clean, meta, err := core.Preprocess(context.Background(), cfg.Directives, src, path)
	if err != nil {
		return nil, fmt.Errorf("preprocess: %w", err)
	}

	topContribs := core.PreprocessContributionsFromMeta(meta, cfg.CompileOpts...)

	// The cleaned source can be whitespace-only when every line was a
	// directive. Parsing it produces "expected expression, got end of
	// input", which is an artifact of a source with no pipeline content,
	// not a real error.
	if strings.TrimSpace(clean) != "" {
		topCtx := macros.WithTrace(context.Background(), trace)
		topCtx = macros.WithWhere(topCtx, "top-level")

		topPrimaries := cfg.PrimariesFor(meta)
		if err := parseWithTrace(topCtx, cfg.Operators, topPrimaries, clean, path); err != nil {
			return trace.Expansions(), err
		}
	}

	pipelines, _ := meta["pipelines"].(map[string]string)
	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		body := pipelines[name]
		fragClean, fragMeta, perr := core.Preprocess(context.Background(), cfg.Directives, body, name)
		if perr != nil {
			return trace.Expansions(), fmt.Errorf("@pipeline %s: %w", name, perr)
		}
		if strings.TrimSpace(fragClean) == "" {
			continue
		}

		fragCtx := macros.WithTrace(context.Background(), trace)
		fragCtx = macros.WithWhere(fragCtx, "pipeline "+name)

		fragPrimaries := cfg.PrimariesFor(fragMeta, topContribs.Primaries...)
		if err := parseWithTrace(fragCtx, cfg.Operators, fragPrimaries, fragClean, name); err != nil {
			return trace.Expansions(), fmt.Errorf("@pipeline %s: %w", name, err)
		}
	}

	return trace.Expansions(), nil
}

func parseWithTrace(
	ctx context.Context,
	ops *core.OperatorTable,
	primaries *core.PrimaryExtensionTable,
	src, name string,
) error {
	_, err := core.NewParserWithFileOffset(ctx, ops, primaries, src, name, 0).Parse()
	return err
}

func parseExpandFilter(args []string) []string {
	var filter []string
	for _, a := range args {
		if names, ok := strings.CutPrefix(a, "--macro="); ok {
			for n := range strings.SplitSeq(names, ",") {
				if n = strings.TrimSpace(n); n != "" {
					filter = append(filter, n)
				}
			}
		}
	}
	return filter
}

func renderExpansions(w io.Writer, exps []macros.Expansion, path string, filter []string) {
	useColor := colorEnabled(w)

	if len(filter) > 0 {
		kept := make([]macros.Expansion, 0, len(exps))
		for _, e := range exps {
			if slices.Contains(filter, e.Macro) {
				kept = append(kept, e)
			}
		}
		exps = kept
	}

	if len(exps) == 0 {
		if len(filter) > 0 {
			fmt.Fprintf(w, "no expansions matched --macro=%s\n", strings.Join(filter, ","))
		}
		return
	}

	fmt.Fprintf(w, "%s\n\n", bold(path, useColor))

	for i := range exps {
		e := &exps[i]

		where := e.Where
		if where == "" {
			where = "top-level"
		}
		fmt.Fprintf(w, "  %s  %s\n",
			cyan("@"+e.Macro, useColor),
			dim(fmt.Sprintf("defined at line %d, invoked in %s at line %d",
				e.DefLine, where, e.InvokeLine), useColor))

		for line := range strings.SplitSeq(e.Body, "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}

		if e.Err != nil {
			fmt.Fprintf(w, "    %s %s\n",
				red("✗", useColor),
				red(e.Err.Error(), useColor))
		}
		fmt.Fprintln(w)
	}
}
