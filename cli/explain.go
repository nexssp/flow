package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/runner"
)

// runExplain prints the effective modifier set for every atom in a
// .nflow file, with each modifier attributed to the lookup that
// produced it. It parses with the same line-indexed modifier chain
// the runtime compiler uses, so its output is what the compiler sees.
func runExplain(ctx context.Context, inv *invocation, args []string) int {
	options, err := parseExplainOptions(args)
	if err != nil {
		return fatalf("usage: nflow explain <file.nflow> [--ast|--actions|--plan] [--json|--dot]: %v", err)
	}
	if _, err := os.Stat(options.path); err != nil {
		return fatalf("read: %v", err)
	}
	return withHarness(ctx, inv, "explain", options.path, args, runExplainInProcess)
}

func runExplainInProcess(ctx context.Context, inv *invocation, args []string) int {
	options, err := parseExplainOptions(args)
	if err != nil {
		return fatalf("usage: nflow explain <file.nflow> [--ast|--actions|--plan] [--json|--dot]: %v", err)
	}
	path := options.path

	src, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read: %v", err)
	}

	reqs, err := inv.sourceRequiresFromFile(ctx, path)
	if err != nil {
		return fatalf("requires: %v", err)
	}

	cfg, err := inv.buildConfig(reqs)
	if err != nil {
		return fatalf("config: %v", err)
	}

	if options.actions {
		if err := renderExplainActionGraph(ctx, os.Stdout, cfg, string(src), path, options.json, options.dot); err != nil {
			return fatalf("%v", err)
		}
		return 0
	}
	if options.plan {
		if err := renderExplainActionPlan(ctx, os.Stdout, cfg, string(src), path, options.json, options.dot); err != nil {
			return fatalf("%v", err)
		}
		return 0
	}

	if options.ast {
		if err := renderExplainASTWithContext(ctx, os.Stdout, cfg, string(src), path, options.json); err != nil {
			return fatalf("%v", err)
		}
		return 0
	}

	if err := renderExplainWithContext(ctx, os.Stdout, cfg, string(src), path); err != nil {
		return fatalf("%v", err)
	}
	return 0
}

type explainOptions struct {
	path    string
	ast     bool
	actions bool
	plan    bool
	json    bool
	dot     bool
}

func parseExplainOptions(args []string) (explainOptions, error) {
	var options explainOptions
	for _, arg := range args {
		if err := applyExplainOption(&options, arg); err != nil {
			return explainOptions{}, err
		}
	}
	if err := validateExplainOptions(options); err != nil {
		return explainOptions{}, err
	}
	return options, nil
}

func applyExplainOption(options *explainOptions, arg string) error {
	switch arg {
	case "--ast":
		options.ast = true
	case "--actions":
		options.actions = true
	case "--plan":
		options.plan = true
	case "--json":
		options.json = true
	case "--dot":
		options.dot = true
	default:
		return setExplainPath(options, arg)
	}
	return nil
}

func setExplainPath(options *explainOptions, path string) error {
	if strings.HasPrefix(path, "-") {
		return fmt.Errorf("unknown flag %q", path)
	}
	if options.path != "" {
		return fmt.Errorf("unexpected argument %q", path)
	}
	options.path = path
	return nil
}

func validateExplainOptions(options explainOptions) error {
	if options.path == "" {
		return errors.New("missing source file")
	}
	views := 0
	for _, selected := range []bool{options.ast, options.actions, options.plan} {
		if selected {
			views++
		}
	}
	if views > 1 {
		return errors.New("--ast, --actions, and --plan select different views")
	}
	if options.dot && !options.actions && !options.plan {
		return errors.New("--dot requires --actions or --plan")
	}
	if options.dot && options.json {
		return errors.New("--dot and --json are mutually exclusive")
	}
	if options.json && !options.ast && !options.actions && !options.plan {
		return errors.New("--json requires --ast, --actions, or --plan")
	}
	return nil
}

// renderExplain is the pure function behind runExplainInProcess. It
// takes a source string and writes the report to w. Tests drive it
// directly, without touching disk.
// renderExplain now computes top-level preprocess contributions once
// and threads them into the pipeline renderer as inherited primaries.
func renderExplain(w io.Writer, cfg runner.Config, src, path string) error {
	return renderExplainWithContext(context.Background(), w, cfg, src, path)
}

func renderExplainWithContext(ctx context.Context, w io.Writer, cfg runner.Config, src, path string) error {
	useColor := colorEnabled(w)

	clean, meta, err := compiler.Preprocess(ctx, cfg.Directives, src, path)
	if err != nil {
		return fmt.Errorf("preprocess: %w", err)
	}

	fmt.Fprintf(w, "%s\n\n", bold(path, useColor))

	topContribs := compiler.PreprocessContributionsFromMeta(meta, cfg.CompileOpts...)

	topLineMods := computeLineMods(cfg, meta)
	renderExplainTopLevel(ctx, w, cfg, clean, meta, path, topLineMods, useColor)
	renderExplainPipelines(ctx, w, cfg, meta, topContribs.Primaries, useColor)
	return nil
}

// renderExplainTopLevel takes meta so it can install per-source
// primaries via cfg.PrimariesFor — reading cfg.Primaries directly
// drops the macro engine and would reject a file that runs.
func renderExplainTopLevel(
	ctx context.Context,
	w io.Writer,
	cfg runner.Config,
	clean string,
	meta map[string]any,
	path string,
	lineMods []compiler.LineLookup,
	useColor bool,
) {
	if strings.TrimSpace(clean) == "" {
		return
	}
	ast, err := compiler.NewParserWithFileOffset(
		ctx, cfg.Operators, cfg.PrimariesFor(meta), clean, path, 0,
	).
		WithLineModifiers(lineMods...).
		Parse()
	if err != nil {
		fmt.Fprintf(w, "  %s\n\n", red(fmt.Sprintf("<parse error: %v>", err), useColor))
		return
	}
	fmt.Fprintf(w, "  %s\n", dim("[top-level]", useColor))
	renderExplainNodes(w, ast, "  ", useColor)
	fmt.Fprintln(w)
}

func renderExplainPipeline(
	ctx context.Context,
	w io.Writer,
	cfg runner.Config,
	name, body string,
	mods []string,
	modSources []compiler.ModifierSource,
	parentPrimaries []compiler.PrimaryExtension,
	useColor bool,
) {
	canonical := name
	if !strings.Contains(canonical, ".") {
		canonical = "pipeline." + canonical
	}

	fmt.Fprintf(w, "  %s  %s\n",
		cyan("@pipeline "+name, useColor),
		dim("[definition]", useColor))

	wrapper, bodyMods := runner.SplitPipelineModifiersWithSources(cfg.Modifiers, mods, modSources)

	fmt.Fprintf(w, "    %s\n", cyan(canonical, useColor))
	if len(wrapper) == 0 {
		fmt.Fprintf(w, "      %s\n", dim("(no wrapper modifiers)", useColor))
	} else {
		for _, sm := range wrapper {
			renderExplainModifier(w, sm.Raw, sm.Source, "      ", useColor)
		}
	}

	clean, fragMeta, err := compiler.Preprocess(ctx, cfg.Directives, body, name)
	if err != nil {
		fmt.Fprintf(w, "    %s\n\n", red(fmt.Sprintf("<preprocess error: %v>", err), useColor))
		return
	}
	if strings.TrimSpace(clean) == "" {
		fmt.Fprintln(w)
		return
	}

	opts := append([]compiler.CompileOption(nil), cfg.CompileOpts...)
	contribs := compiler.PreprocessContributionsFromMeta(fragMeta, cfg.CompileOpts...)
	opts = append(opts, contribs.CompileOpts...)

	if len(bodyMods) > 0 {
		// Group body modifiers by their origin so each source keeps its
		// attribution through the parser's lookup chain.
		bySource := make(map[compiler.ModifierSource][]string)
		order := make([]compiler.ModifierSource, 0, len(bodyMods))
		for _, sm := range bodyMods {
			if _, seen := bySource[sm.Source]; !seen {
				order = append(order, sm.Source)
			}
			bySource[sm.Source] = append(bySource[sm.Source], sm.Raw)
		}
		for _, src := range order {
			raws := bySource[src]
			opts = append(opts, compiler.WithLineModifiers(compiler.LineLookup{
				Source: src,
				Fn:     func(int) []string { return raws },
			}))
		}
	}

	lineMods := compiler.LineModifiersFromOptions(cfg.Modifiers, opts)

	// A pipeline body sees its own macros plus the parent's, the same
	// way the runtime compiler stacks CompileReq.InheritedPrimaries.
	fragPrimaries := cfg.PrimariesFor(fragMeta, parentPrimaries...)

	ast, perr := compiler.NewParserWithFileOffset(
		ctx, cfg.Operators, fragPrimaries, clean, name, 0,
	).
		WithLineModifiers(lineMods...).
		Parse()
	if perr != nil {
		fmt.Fprintf(w, "    %s\n\n", red(fmt.Sprintf("<parse error: %v>", perr), useColor))
		return
	}

	renderExplainNodes(w, ast, "    ", useColor)
	fmt.Fprintln(w)
}

func renderExplainNodes(w io.Writer, ast compiler.Expr, indent string, useColor bool) {
	rendered := false
	first := true
	walkExplainNodes(ast, func(node compiler.Expr) {
		if !first {
			fmt.Fprintln(w)
		}
		first = false
		rendered = true
		switch n := node.(type) {
		case *compiler.Atom:
			renderExplainAtom(w, n, indent, useColor)
		case *compiler.ProjectionExpr:
			renderExplainProjection(w, n, indent, useColor)
		}
	})
	if !rendered {
		fmt.Fprintf(w, "%s%s\n", indent, dim("(no atoms or projections)", useColor))
	}
}

func renderExplainAtom(w io.Writer, a *compiler.Atom, indent string, useColor bool) {
	label := ""
	if strings.HasPrefix(a.Name, "pipeline.") {
		label = "  " + dim("[invocation]", useColor)
	}
	fmt.Fprintf(w, "%s%s%s\n", indent, cyan(a.Name, useColor), label)
	if len(a.Modifiers) == 0 {
		fmt.Fprintf(w, "%s  %s\n", indent, dim("(no modifiers)", useColor))
		return
	}
	for i, raw := range a.Modifiers {
		var src compiler.ModifierSource
		if i < len(a.ModifierSources) {
			src = a.ModifierSources[i]
		}
		renderExplainModifier(w, raw, src, indent+"  ", useColor)
	}
}

func renderExplainProjection(w io.Writer, p *compiler.ProjectionExpr, indent string, useColor bool) {
	body := strings.TrimSpace(p.Raw)
	fmt.Fprintf(w, "%s%s\n", indent, cyan("{ projection }", useColor))
	if body == "" {
		fmt.Fprintf(w, "%s  %s\n", indent, dim("(empty)", useColor))
		return
	}
	for field := range strings.SplitSeq(body, ",") {
		if f := strings.TrimSpace(field); f != "" {
			fmt.Fprintf(w, "%s  %s\n", indent, dim(f, useColor))
		}
	}
}

func renderExplainModifier(w io.Writer, raw string, src compiler.ModifierSource, indent string, useColor bool) {
	name := compiler.ModifierName(raw)
	value := ""
	if i := strings.IndexByte(raw, '='); i > 0 {
		value = raw[i+1:]
	}
	display := ":" + name
	if value != "" {
		display += "=" + value
	}

	srcText := src.Kind
	if src.Label != "" {
		srcText = src.Kind + " " + src.Label
	}
	if srcText == "" {
		srcText = "unknown"
	}

	fmt.Fprintf(w, "%s%-24s %s\n",
		indent,
		cyan(display, useColor),
		green("from "+srcText, useColor))
}

// walkExplainNodes visits atoms and projections in document order. It
// descends into ProjectionExpr so the report reflects the whole shape
// of the file, not just its atoms.
func walkExplainNodes(node compiler.Expr, visit func(compiler.Expr)) {
	switch n := node.(type) {
	case *compiler.Atom:
		visit(n)
	case *compiler.ProjectionExpr:
		visit(n)
	case *compiler.PipeExpr:
		walkExplainNodes(n.L, visit)
		walkExplainNodes(n.R, visit)
	case *compiler.ParallelExpr:
		for _, c := range n.Branches {
			walkExplainNodes(c, visit)
		}
	case *compiler.FallbackExpr:
		walkExplainNodes(n.L, visit)
		walkExplainNodes(n.R, visit)
	case *compiler.ConditionalExpr:
		walkExplainNodes(n.Cond, visit)
		walkExplainNodes(n.Then, visit)
		if n.Else != nil {
			walkExplainNodes(n.Else, visit)
		}
	case *compiler.LoopExpr:
		walkExplainNodes(n.Body, visit)
	case *compiler.AssertExpr:
		// Assert carries a raw expression, not a callable node.
	}
}

// renderExplainPipelines now receives the top-level preprocess
// primaries as parentPrimaries and forwards them to each pipeline
// body, mirroring CompileReq.InheritedPrimaries.
func renderExplainPipelines(
	ctx context.Context,
	w io.Writer,
	cfg runner.Config,
	meta map[string]any,
	parentPrimaries []compiler.PrimaryExtension,
	useColor bool,
) {
	pipelines, _ := meta["pipelines"].(map[string]string)
	if len(pipelines) == 0 {
		return
	}

	pipelineMods, _ := meta["pipeline_modifiers"].(map[string][]string)
	pipelineModSources, _ := meta["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)

	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		renderExplainPipeline(
			ctx, w, cfg, name, pipelines[name],
			pipelineMods[name], pipelineModSources[name],
			parentPrimaries, useColor,
		)
	}
}
