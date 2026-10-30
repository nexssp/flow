package projection

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

// composedProjection keeps Flow values as real AST children. The surrounding
// object stays an expr-lang expression, including its lazy conditional semantics.
type composedProjection struct {
	raw      string
	bindings []projectionBinding
	pos      compiler.Position
	ctxName  string
}

type projectionBinding struct {
	name string
	node compiler.Expr
}

func (*composedProjection) Node() {}

func (p *composedProjection) Children() []compiler.Expr {
	children := make([]compiler.Expr, len(p.bindings))
	for i, binding := range p.bindings {
		children[i] = binding.node
	}
	return children
}

func (p *composedProjection) Analyze(resolver compiler.CapabilityResolver) error {
	for _, binding := range p.bindings {
		if err := binding.node.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (*composedProjection) DescribeActionGraph() compiler.ActionGraphDescription {
	return compiler.ActionGraphDescription{Kind: "projection", Name: "projection.project"}
}

func (p *composedProjection) Build(ctx context.Context, bCtx *compiler.BuildContext) (action.AnyAction, error) {
	options := []expr.Option{expr.AllowUndefinedVariables(), spreadMergeOption}
	children := make([]action.IntrospectionChild, 0, len(p.bindings))
	for _, binding := range p.bindings {
		child, err := binding.node.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		options = append(options, expr.Function(binding.name, func(params ...any) (any, error) { //nolint:contextcheck // Execution context is supplied by expr.Run, never the build context.
			callCtx, ok := params[0].(context.Context)
			if !ok {
				return nil, xerr.Internal("projection: missing execution context")
			}
			return action.InvokeAny(callCtx, child, params[1])
		}))
		if bCtx.Introspect {
			children = append(children, action.IntrospectionChild{Node: child, Relation: "value", Role: "value"})
		}
	}
	source, err := buildProgramSource(p.raw)
	if err != nil {
		return nil, compiler.SourceError(p.pos, "projection: %v", err)
	}
	program, err := compiler.CompileExpression(source, options...)
	if err != nil {
		return nil, compiler.SourceError(p.pos, "projection: %v", err)
	}
	compiled := action.New("projection.project", func(callCtx context.Context, input any) (any, error) {
		if contextErr := callCtx.Err(); contextErr != nil {
			return nil, contextErr
		}
		env := compiler.MergeSymbolNamespaces(callCtx, buildEnv(input))
		env[p.ctxName] = callCtx
		out, runErr := expr.Run(program, env)
		if runErr != nil {
			appErr, classified := errors.AsType[*xerr.AppError](runErr)
			if classified && appErr != nil ||
				errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				return nil, runErr
			}
			return nil, xerr.BadRequest("projection: evaluation failed", runErr)
		}
		return out, nil
	}).Build()
	return compiler.TrackActionComposition(bCtx, compiled, action.IntrospectionComposition{
		Kind: "projection", Children: children,
	}), nil
}

// parseProjectionBody walks the original parser, not a second grammar. A Flow
// primary in value position is parsed by its registered extension, preserving
// source positions, inherited modifiers, capability discovery and nested syntax.
func parseProjectionBody(p *compiler.Parser, closeIndex int, pos compiler.Position) (compiler.Expr, error) {
	src := p.Src()
	start := p.Cur().Offset + 1
	cursor := start
	var rewritten strings.Builder
	var bindings []projectionBinding
	prefix := "__flow_projection_"
	for strings.Contains(src[start:closeIndex], prefix) {
		prefix += "_"
	}
	ctxName := prefix + "context__"
	p.Next() // opening brace
	for p.Cur().Type != compiler.TokEOF && p.Cur().Offset < closeIndex {
		if !isFlowValue(p) {
			p.Next()
			continue
		}
		valueStart := p.Cur().Offset
		// Only consume the primary and its policy modifiers here. Expr-lang
		// retains ownership of the surrounding ternary, arithmetic and calls.
		node, err := p.ParseExpr(int(^uint(0) >> 1))
		if err != nil {
			return nil, err
		}
		end := p.Cur().Offset
		if end > closeIndex {
			return nil, p.Fail(p.Cur().Line, "projection: Flow value crosses object boundary")
		}
		name := fmt.Sprintf("%svalue_%d__", prefix, len(bindings))
		rewritten.WriteString(src[cursor:valueStart])
		fmt.Fprintf(&rewritten, "%s(%s, __root__)", name, ctxName)
		// Keep subsequent diagnostics aligned with the original file.
		rewritten.WriteString(strings.Repeat("\n", strings.Count(src[valueStart:end], "\n")))
		bindings = append(bindings, projectionBinding{name: name, node: node})
		cursor = end
	}
	rewritten.WriteString(src[cursor:closeIndex])
	if p.Cur().Type == compiler.TokRBrace && p.Cur().Offset == closeIndex {
		p.Next()
	}
	raw := stripHashComments(rewritten.String())
	if len(bindings) == 0 {
		return &compiler.ProjectionExpr{Raw: raw, Pos: pos}, nil
	}
	return &composedProjection{raw: raw, bindings: bindings, pos: pos, ctxName: ctxName}, nil
}

func isFlowValue(p *compiler.Parser) bool {
	if p.Cur().Type != compiler.TokIdent {
		return false
	}
	switch p.Cur().Lit {
	case "parallel", "race":
		return p.Peek(1).Type == compiler.TokLBrace
	case "match":
		return p.Peek(1).Type == compiler.TokLParen || p.Peek(1).Type == compiler.TokLBrace
	default:
		return false
	}
}
