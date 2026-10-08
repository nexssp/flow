package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/runner"
)

// ── public entry ─────────────────────────────────────────────────────

func renderExplainASTWithContext(ctx context.Context, w io.Writer, cfg runner.Config, src, path string, jsonOut bool) error {
	topAST, pipelines, err := parseExplainASTWithContext(ctx, cfg, src, path)
	if err != nil {
		return err
	}

	if jsonOut {
		return writeExplainASTJSON(w, path, topAST, pipelines)
	}
	renderExplainASTText(w, path, topAST, pipelines)
	return nil
}

// ── parsing ──────────────────────────────────────────────────────────

// parseExplainAST preprocesses the source and returns the top-level
// AST plus one AST per @pipeline body. It mirrors the parse path the
// runtime compiler uses:
//
//   - Preprocess strips directives and yields meta.
//   - The cleaned top-level source is parsed with cfg.PrimariesFor(meta)
//     so per-source primaries (the macro engine) are installed.
//   - Each @pipeline body is preprocessed in isolation, then parsed
//     with the parent's primaries stacked in front of its own —
//     mirroring CompileReq.InheritedPrimaries.
//
// A source whose cleaned top-level is whitespace-only (a file that
// contains only @pipeline definitions) yields a nil topAST. That is
// not an error: the pipeline bodies are still rendered.
func parseExplainASTWithContext(
	ctx context.Context, cfg runner.Config, src, path string,
) (topAST core.Expr, pipelines map[string]core.Expr, err error) {
	clean, meta, err := core.Preprocess(ctx, cfg.Directives, src, path)
	if err != nil {
		return nil, nil, fmt.Errorf("preprocess: %w", err)
	}

	if strings.TrimSpace(clean) != "" {
		ast, parseErr := core.NewParserWithFileOffset(
			ctx, cfg.Operators, cfg.PrimariesFor(meta), clean, path, 0,
		).Parse()
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse: %w", parseErr)
		}
		topAST = ast
	}

	rawPipelines, _ := meta["pipelines"].(map[string]string)
	topContribs := core.PreprocessContributionsFromMeta(meta, cfg.CompileOpts...)

	pipelines = make(map[string]core.Expr, len(rawPipelines))
	for name, body := range rawPipelines {
		fragClean, fragMeta, perr := core.Preprocess(ctx, cfg.Directives, body, name)
		if perr != nil {
			return nil, nil, fmt.Errorf("@pipeline %s: preprocess: %w", name, perr)
		}
		if strings.TrimSpace(fragClean) == "" {
			continue
		}
		fragPrimaries := cfg.PrimariesFor(fragMeta, topContribs.Primaries...)
		ast, parseErr := core.NewParserWithFileOffset(
			ctx, cfg.Operators, fragPrimaries, fragClean, name, 0,
		).Parse()
		if parseErr != nil {
			return nil, nil, fmt.Errorf("@pipeline %s: %w", name, parseErr)
		}
		pipelines[name] = ast
	}

	return topAST, pipelines, nil
}

// ── text rendering ───────────────────────────────────────────────────

func renderExplainASTText(w io.Writer, path string, topAST core.Expr, pipelines map[string]core.Expr) {
	useColor := colorEnabled(w)

	fmt.Fprintf(w, "%s\n\n", bold(path, useColor))

	if topAST != nil {
		fmt.Fprintf(w, "  %s\n", dim("[top-level]", useColor))
		renderASTNode(w, topAST, "  ", useColor)
		fmt.Fprintln(w)
	}

	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(w, "  @pipeline %s  %s\n",
			name, dim("[definition]", useColor))
		renderASTNode(w, pipelines[name], "  ", useColor)
		fmt.Fprintln(w)
	}
}

// renderASTNode walks one node and prints it indented. Composite
// nodes print a header line and recurse into children; leaves print a
// single line with the relevant payload.
//
// The operators that appear in the header lines (->, &, ||, ?:) are
// the source forms a reader wrote, so the tree reads the same way the
// file does.
func renderASTNode(w io.Writer, expr core.Expr, indent string, useColor bool) {
	if expr == nil {
		fmt.Fprintf(w, "%s%s\n", indent, dim("(nil)", useColor))
		return
	}

	switch n := expr.(type) {
	case *core.Atom:
		fmt.Fprintf(w, "%s%-12s %s%s\n",
			indent,
			cyan("Atom", useColor),
			cyan(n.Name, useColor),
			formatASTArgs(n.Args, useColor),
		)

	case *core.PipeExpr:
		fmt.Fprintf(w, "%s%s\n", indent, dim("Pipe ->", useColor))
		renderASTNode(w, n.L, indent+"  ", useColor)
		renderASTNode(w, n.R, indent+"  ", useColor)

	case *core.ParallelExpr:
		fmt.Fprintf(w, "%s%s\n", indent, dim("Parallel &", useColor))
		for _, b := range n.Branches {
			renderASTNode(w, b, indent+"  ", useColor)
		}

	case *core.FallbackExpr:
		fmt.Fprintf(w, "%s%s\n", indent, dim("Fallback ||", useColor))
		renderASTNode(w, n.L, indent+"  ", useColor)
		renderASTNode(w, n.R, indent+"  ", useColor)

	case *core.ConditionalExpr:
		fmt.Fprintf(w, "%s%s\n", indent, dim("Conditional ?:", useColor))
		fmt.Fprintf(w, "%s  %s\n", indent, dim("Cond:", useColor))
		renderASTNode(w, n.Cond, indent+"    ", useColor)
		fmt.Fprintf(w, "%s  %s\n", indent, dim("Then:", useColor))
		renderASTNode(w, n.Then, indent+"    ", useColor)
		if n.Else != nil {
			fmt.Fprintf(w, "%s  %s\n", indent, dim("Else:", useColor))
			renderASTNode(w, n.Else, indent+"    ", useColor)
		}

	case *core.ProjectionExpr:
		raw := strings.TrimSpace(n.Raw)
		if len(raw) > 60 {
			raw = raw[:57] + "..."
		}
		fmt.Fprintf(w, "%s%-12s %s\n",
			indent,
			cyan("Projection", useColor),
			dim("{ "+raw+" }", useColor),
		)

	case *core.LoopExpr:
		fmt.Fprintf(w, "%s%-12s %s\n",
			indent,
			cyan("Loop", useColor),
			dim("until("+n.Until+")", useColor),
		)
		renderASTNode(w, n.Body, indent+"  ", useColor)

	case *core.AssertExpr:
		msg := ""
		if n.Message != "" {
			msg = " " + dim(`"`+n.Message+`"`, useColor)
		}
		fmt.Fprintf(w, "%s%-12s %s%s\n",
			indent,
			cyan("Assert", useColor),
			dim(n.Condition, useColor),
			msg,
		)

	default:
		fmt.Fprintf(w, "%s%s\n", indent,
			dim(fmt.Sprintf("Unknown(%T)", expr), useColor))
	}
}

// formatASTArgs renders the @{...} payload for an atom on one line.
// Keys are sorted so two runs of the same file produce identical
// output; a map iteration order leak would make the view useless in a
// diff.
func formatASTArgs(args map[string]*core.Value, useColor bool) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("  @{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(" ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(formatASTValue(args[k]))
	}
	b.WriteString(" }")
	return dim(b.String(), useColor)
}

// formatASTValue renders one argument value in its source form.
// Composite values (maps, slices) are elided: their full shape would
// dominate the line, and the JSON output carries the full tree when
// detail is needed.
func formatASTValue(v *core.Value) string {
	if v == nil {
		return "nil"
	}
	switch v.Kind {
	case core.ValueString:
		return fmt.Sprintf("%q", v.Str)
	case core.ValueBare:
		return v.Str
	case core.ValueNumber:
		return fmt.Sprintf("%g", v.Num)
	case core.ValueBool:
		return strconv.FormatBool(v.Bool)
	case core.ValueNull:
		return "null"
	case core.ValueRef:
		if v.Ref == "" {
			return "."
		}
		return "." + v.Ref
	case core.ValueMap:
		return "{...}"
	case core.ValueSlice:
		return "[...]"
	}
	return "?"
}

type ASTPosition struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// ── JSON rendering ───────────────────────────────────────────────────

// ASTNode is the machine-readable form of one AST node. Fields are
// populated selectively based on Kind:
//
//	Atom          — Name, Args
//	Pipe          — L, R
//	Parallel      — Branches
//	Fallback      — L, R
//	Conditional   — Cond, Then, Else
//	Projection    — Raw
//	Loop          — Body, Until
//	Assert        — Condition, Message
//
// A consumer that switches on Kind only reads the fields the Kind
// documents. Unknown kinds carry only Kind, which is the extension
// point: a future AST node type adds a new Kind string and its own
// fields without breaking existing consumers.
type ASTNode struct {
	Kind string       `json:"kind"`
	Pos  *ASTPosition `json:"pos,omitempty"`

	Name string         `json:"name,omitempty"`
	Args map[string]any `json:"args,omitempty"`

	Raw       string `json:"raw,omitempty"`
	Condition string `json:"condition,omitempty"`
	Message   string `json:"message,omitempty"`
	Until     string `json:"until,omitempty"`

	L        *ASTNode   `json:"l,omitempty"`
	R        *ASTNode   `json:"r,omitempty"`
	Cond     *ASTNode   `json:"cond,omitempty"`
	Then     *ASTNode   `json:"then,omitempty"`
	Else     *ASTNode   `json:"else,omitempty"`
	Body     *ASTNode   `json:"body,omitempty"`
	Branches []*ASTNode `json:"branches,omitempty"`
}

// ASTPipeline is one named pipeline definition.
type ASTPipeline struct {
	Name string   `json:"name"`
	Body *ASTNode `json:"body"`
}

// ASTReport is the top-level JSON envelope. TopLevel is omitted when
// the file contains only @pipeline definitions.
type ASTReport struct {
	Path      string        `json:"path"`
	TopLevel  *ASTNode      `json:"top_level,omitempty"`
	Pipelines []ASTPipeline `json:"pipelines,omitempty"`
}

func writeExplainASTJSON(
	w io.Writer, path string, topAST core.Expr, pipelines map[string]core.Expr,
) error {
	report := ASTReport{
		Path:     path,
		TopLevel: astNodeOf(topAST),
	}

	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		report.Pipelines = append(report.Pipelines, ASTPipeline{
			Name: name,
			Body: astNodeOf(pipelines[name]),
		})
	}

	return writeJSON(w, report, true)
}

func astNodeOf(expr core.Expr) *ASTNode {
	if expr == nil {
		return nil
	}
	node := buildASTNode(expr)
	if p := resolveExprPos(expr); p != (core.Position{}) {
		node.Pos = &ASTPosition{File: p.File, Line: p.Line, Col: p.Col}
	}
	return node
}

// buildASTNode lowers one expression to its machine-readable shape,
// without touching positions — that step is handled by astNodeOf, which
// can also walk children when a composite node has no explicit Pos.
func buildASTNode(expr core.Expr) *ASTNode {
	switch n := expr.(type) {
	case *core.Atom:
		var args map[string]any
		if len(n.Args) > 0 {
			args = make(map[string]any, len(n.Args))
			for k, v := range n.Args {
				args[k] = valueToAny(v)
			}
		}
		return &ASTNode{Kind: "Atom", Name: n.Name, Args: args}

	case *core.PipeExpr:
		return &ASTNode{Kind: "Pipe", L: astNodeOf(n.L), R: astNodeOf(n.R)}

	case *core.ParallelExpr:
		branches := make([]*ASTNode, len(n.Branches))
		for i, b := range n.Branches {
			branches[i] = astNodeOf(b)
		}
		return &ASTNode{Kind: "Parallel", Branches: branches}

	case *core.FallbackExpr:
		return &ASTNode{Kind: "Fallback", L: astNodeOf(n.L), R: astNodeOf(n.R)}

	case *core.ConditionalExpr:
		return &ASTNode{
			Kind: "Conditional",
			Cond: astNodeOf(n.Cond),
			Then: astNodeOf(n.Then),
			Else: astNodeOf(n.Else),
		}

	case *core.ProjectionExpr:
		return &ASTNode{Kind: "Projection", Raw: n.Raw}

	case *core.LoopExpr:
		return &ASTNode{Kind: "Loop", Body: astNodeOf(n.Body), Until: n.Until}

	case *core.AssertExpr:
		return &ASTNode{Kind: "Assert", Condition: n.Condition, Message: n.Message}
	}
	return &ASTNode{Kind: fmt.Sprintf("Unknown(%T)", expr)}
}

// resolveExprPos returns the node's own Pos when set, otherwise the
// leftmost non-zero Pos in its subtree. Composite nodes built by operator
// handlers often leave Pos zero; this makes the JSON useful anyway.
func resolveExprPos(e core.Expr) core.Position {
	if e == nil {
		return core.Position{}
	}
	switch n := e.(type) {
	case *core.Atom:
		return n.Pos
	case *core.ProjectionExpr:
		return n.Pos
	case *core.AssertExpr:
		return n.Pos
	case *core.LoopExpr:
		if n.Pos != (core.Position{}) {
			return n.Pos
		}
		return resolveExprPos(n.Body)
	case *core.PipeExpr:
		if n.Pos != (core.Position{}) {
			return n.Pos
		}
		return resolveExprPos(n.L)
	case *core.FallbackExpr:
		if n.Pos != (core.Position{}) {
			return n.Pos
		}
		if p := resolveExprPos(n.L); p != (core.Position{}) {
			return p
		}
		return resolveExprPos(n.R)
	case *core.ConditionalExpr:
		if n.Pos != (core.Position{}) {
			return n.Pos
		}
		for _, c := range []core.Expr{n.Cond, n.Then, n.Else} {
			if p := resolveExprPos(c); p != (core.Position{}) {
				return p
			}
		}
	case *core.ParallelExpr:
		if n.Pos != (core.Position{}) {
			return n.Pos
		}
		for _, b := range n.Branches {
			if p := resolveExprPos(b); p != (core.Position{}) {
				return p
			}
		}
	}
	return core.Position{}
}

func valueToAny(v *core.Value) any {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case core.ValueString:
		return v.Str
	case core.ValueBare:
		return v.Str
	case core.ValueNumber:
		return v.Num
	case core.ValueBool:
		return v.Bool
	case core.ValueNull:
		return nil
	case core.ValueRef:
		if v.Ref == "" {
			return "."
		}
		return "." + v.Ref
	case core.ValueMap:
		m := make(map[string]any, len(v.Map))
		for _, e := range v.Map {
			m[e.Key] = valueToAny(e.Value)
		}
		return m
	case core.ValueSlice:
		s := make([]any, len(v.Slice))
		for i, e := range v.Slice {
			s[i] = valueToAny(e)
		}
		return s
	}
	return nil
}
