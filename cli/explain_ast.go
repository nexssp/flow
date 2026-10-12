package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	core "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/runner"
)

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

	case *core.LiteralExpr:
		fmt.Fprintf(w, "%s%-12s %v\n",
			indent,
			cyan("Literal", useColor),
			n.Val,
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

type ASTPipeline struct {
	Name string   `json:"name"`
	Body *ASTNode `json:"body"`
}

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

	case *core.LiteralExpr:
		return &ASTNode{Kind: "Literal", Name: fmt.Sprintf("%v", n.Val)}

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

// resolveExprPos returns the source position to attribute to expr: its
// own stored position when set, otherwise the first non-zero position
// recovered from the specific children the original traversal
// considered. Returns the zero Position for a nil expression or when
// nothing in the considered subtree carries a position.
//
// The traversal order is load-bearing for diagnostics and is pinned by
// resolveExprPos tests: PipeExpr and LoopExpr only descend into their
// left / body arm respectively, FallbackExpr tries L then R,
// ConditionalExpr tries Cond then Then then Else, ParallelExpr tries
// each branch in order, and DecoratedExpr contributes no position (it
// was never part of the original traversal).
func resolveExprPos(e core.Expr) core.Position {
	if e == nil {
		return core.Position{}
	}
	if p, ok := leafPos(e); ok {
		return p
	}
	if p, ok := compositePos(e); ok {
		return p
	}
	return core.Position{}
}

// leafPos handles the expression kinds that carry their own position
// directly and unconditionally — a zero position is still returned as
// the node's own, not as "not found". Every other expression kind
// returns false so resolveExprPos falls through to compositePos.
func leafPos(e core.Expr) (core.Position, bool) {
	switch n := e.(type) {
	case *core.Atom:
		return n.Pos, true
	case *core.LiteralExpr:
		return n.Pos, true
	case *core.ProjectionExpr:
		return n.Pos, true
	case *core.AssertExpr:
		return n.Pos, true
	default:
		return core.Position{}, false
	}
}

// compositePos handles expression kinds whose position may be stored
// locally or recovered from a specific subset of children. The subset
// for each kind matches the original single-function switch; changing
// it would change which errors are attributed to which line.
func compositePos(e core.Expr) (core.Position, bool) {
	switch n := e.(type) {
	case *core.LoopExpr:
		return ownOrChild(n.Pos, n.Body)
	case *core.PipeExpr:
		return ownOrChild(n.Pos, n.L)
	case *core.FallbackExpr:
		return ownOrChild(n.Pos, n.L, n.R)
	case *core.ConditionalExpr:
		return ownOrChild(n.Pos, n.Cond, n.Then, n.Else)
	case *core.ParallelExpr:
		return ownOrChild(n.Pos, n.Branches...)
	default:
		return core.Position{}, false
	}
}

// ownOrChild returns pos when it is set, otherwise the first non-zero
// position found by walking children in the given order. The variadic
// signature lets compositePos express each kind's traversal directly;
// nil children (ConditionalExpr.Else can be nil) are skipped by
// resolveExprPos.
func ownOrChild(pos core.Position, children ...core.Expr) (core.Position, bool) {
	if pos != (core.Position{}) {
		return pos, true
	}
	for _, c := range children {
		if p := resolveExprPos(c); p != (core.Position{}) {
			return p, true
		}
	}
	return core.Position{}, false
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
