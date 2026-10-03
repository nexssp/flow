package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/runner"
)

// runExplain prints the effective modifier set for every atom in a
// .nflow file, with each modifier attributed to the lookup that
// produced it. It parses with the same line-indexed modifier chain
// the runtime compiler uses, so its output is what the compiler sees.
func runExplain(args []string) int {
	if len(args) == 0 {
		return fatalf("usage: nflow explain <file.nflow>")
	}
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		return fatalf("read: %v", err)
	}
	return withHarness("explain", path, args, runExplainInProcess)
}

func runExplainInProcess(args []string) int {
	if len(args) < 1 {
		return fatalf("usage: nflow explain <file.nflow>")
	}
	path := args[0]

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

	if err := renderExplain(os.Stdout, cfg, string(src), path); err != nil {
		return fatalf("%v", err)
	}
	return 0
}

// renderExplain is the pure function behind runExplainInProcess. It
// takes a source string and writes the report to w. Tests drive it
// directly, without touching disk.
func renderExplain(w io.Writer, cfg runner.Config, src, path string) error {
	useColor := colorEnabled(w)

	clean, meta, err := core.Preprocess(context.Background(), cfg.Directives, src, path)
	if err != nil {
		return fmt.Errorf("preprocess: %w", err)
	}

	fmt.Fprintf(w, "%s\n\n", bold(path, useColor))

	topLineMods := computeLineMods(cfg, meta)
	renderExplainTopLevel(w, cfg, clean, path, topLineMods, useColor)
	renderExplainPipelines(w, cfg, meta, useColor)
	return nil
}

func renderExplainTopLevel(w io.Writer, cfg runner.Config, clean, path string, lineMods []core.LineLookup, useColor bool) {
	if strings.TrimSpace(clean) == "" {
		return
	}
	ast, err := core.NewParserWithFileOffset(
		context.Background(), cfg.Operators, cfg.Primaries, clean, path, 0,
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

func renderExplainPipelines(w io.Writer, cfg runner.Config, meta map[string]any, useColor bool) {
	pipelines, _ := meta["pipelines"].(map[string]string)
	if len(pipelines) == 0 {
		return
	}

	pipelineMods, _ := meta["pipeline_modifiers"].(map[string][]string)
	pipelineModSources, _ := meta["pipeline_modifier_sources"].(map[string][]core.ModifierSource)

	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		renderExplainPipeline(w, cfg, name, pipelines[name], pipelineMods[name], pipelineModSources[name], useColor)
	}
}

func renderExplainPipeline(
	w io.Writer,
	cfg runner.Config,
	name, body string,
	mods []string,
	modSources []core.ModifierSource,
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

	clean, fragMeta, err := core.Preprocess(context.Background(), cfg.Directives, body, name)
	if err != nil {
		fmt.Fprintf(w, "    %s\n\n", red(fmt.Sprintf("<preprocess error: %v>", err), useColor))
		return
	}
	if strings.TrimSpace(clean) == "" {
		fmt.Fprintln(w)
		return
	}

	opts := append([]core.CompileOption(nil), cfg.CompileOpts...)
	contribs := core.PreprocessContributionsFromMeta(fragMeta, cfg.CompileOpts...)
	opts = append(opts, contribs.CompileOpts...)

	if len(bodyMods) > 0 {
		// Group body modifiers by their origin so each source keeps its
		// attribution through the parser's lookup chain.
		bySource := make(map[core.ModifierSource][]string)
		order := make([]core.ModifierSource, 0, len(bodyMods))
		for _, sm := range bodyMods {
			if _, seen := bySource[sm.Source]; !seen {
				order = append(order, sm.Source)
			}
			bySource[sm.Source] = append(bySource[sm.Source], sm.Raw)
		}
		for _, src := range order {
			raws := bySource[src]
			opts = append(opts, core.WithLineModifiers(core.LineLookup{
				Source: src,
				Fn:     func(int) []string { return raws },
			}))
		}
	}

	lineMods := core.LineModifiersFromOptions(cfg.Modifiers, opts)

	ast, perr := core.NewParserWithFileOffset(
		context.Background(), cfg.Operators, cfg.Primaries, clean, name, 0,
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

func renderExplainNodes(w io.Writer, ast core.Expr, indent string, useColor bool) {
	rendered := false
	first := true
	walkExplainNodes(ast, func(node core.Expr) {
		if !first {
			fmt.Fprintln(w)
		}
		first = false
		rendered = true
		switch n := node.(type) {
		case *core.Atom:
			renderExplainAtom(w, n, indent, useColor)
		case *core.ProjectionExpr:
			renderExplainProjection(w, n, indent, useColor)
		}
	})
	if !rendered {
		fmt.Fprintf(w, "%s%s\n", indent, dim("(no atoms or projections)", useColor))
	}
}

func renderExplainAtom(w io.Writer, a *core.Atom, indent string, useColor bool) {
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
		var src core.ModifierSource
		if i < len(a.ModifierSources) {
			src = a.ModifierSources[i]
		}
		renderExplainModifier(w, raw, src, indent+"  ", useColor)
	}
}

func renderExplainProjection(w io.Writer, p *core.ProjectionExpr, indent string, useColor bool) {
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

func renderExplainModifier(w io.Writer, raw string, src core.ModifierSource, indent string, useColor bool) {
	name, value := splitRawModifier(raw)
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

// walkExplainNodes visits atoms and projections in document order.
// Unlike walkLintAST it descends into ProjectionExpr so the report
// reflects the whole shape of the file, not just its atoms.
func walkExplainNodes(node core.Expr, visit func(core.Expr)) {
	switch n := node.(type) {
	case *core.Atom:
		visit(n)
	case *core.ProjectionExpr:
		visit(n)
	case *core.PipeExpr:
		walkExplainNodes(n.L, visit)
		walkExplainNodes(n.R, visit)
	case *core.ParallelExpr:
		for _, c := range n.Branches {
			walkExplainNodes(c, visit)
		}
	case *core.FallbackExpr:
		walkExplainNodes(n.L, visit)
		walkExplainNodes(n.R, visit)
	case *core.ConditionalExpr:
		walkExplainNodes(n.Cond, visit)
		walkExplainNodes(n.Then, visit)
		if n.Else != nil {
			walkExplainNodes(n.Else, visit)
		}
	case *core.LoopExpr:
		walkExplainNodes(n.Body, visit)
	case *core.AssertExpr:
		// Assert carries a raw expression, not a callable node.
	}
}
