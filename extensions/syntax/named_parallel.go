package syntax

import (
	"context"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

const parallelKeyword = "parallel"

type namedParallelKeyword struct{}

func (namedParallelKeyword) Name() string    { return "named_parallel" }
func (namedParallelKeyword) Keyword() string { return parallelKeyword }

func (namedParallelKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	p.Advance() // consume "parallel"
	if p.Current().Type != compiler.TokLBrace {
		return nil, p.Fail(p.Current().Line, "expected '{' after parallel")
	}
	p.Advance()

	if p.Current().Type == compiler.TokRBrace {
		return nil, p.Fail(p.Current().Line, "parallel block must contain at least one branch")
	}

	branches := make([]namedParallelBranch, 0, 2)
	seen := make(map[string]struct{})
	for {
		labelToken := p.Current()
		if labelToken.Type != compiler.TokIdent {
			return nil, p.Fail(labelToken.Line, "expected parallel branch label")
		}
		label := labelToken.Lit
		if _, exists := seen[label]; exists {
			return nil, p.Fail(labelToken.Line, "duplicate parallel branch label %q", label)
		}
		seen[label] = struct{}{}
		p.Advance()

		if p.Current().Type != compiler.TokColon {
			return nil, p.Fail(p.Current().Line, "expected ':' after parallel branch label %q", label)
		}
		p.Advance()

		branchExpr, err := p.ParseExpr(0)
		if err != nil {
			return nil, err
		}
		branches = append(branches, namedParallelBranch{label: label, expr: branchExpr})

		switch p.Current().Type {
		case compiler.TokComma:
			p.Advance()
			if p.Current().Type == compiler.TokRBrace {
				p.Advance()
				return &NamedParallelExpr{branches: branches}, nil
			}
		case compiler.TokRBrace:
			p.Advance()
			return &NamedParallelExpr{branches: branches}, nil
		case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
			compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr,
			compiler.TokLParen, compiler.TokRParen, compiler.TokLBrace, compiler.TokColon,
			compiler.TokEquals, compiler.TokQuestion, compiler.TokHash, compiler.TokTilde,
			compiler.TokAtBrace, compiler.TokAtPrompt:
			return nil, p.Fail(p.Current().Line, "expected ',' or '}' after parallel branch %q", label)
		}
	}
}

type namedParallelBranch struct {
	label string
	expr  compiler.Expr
}

// NamedParallelExpr runs expressions concurrently and returns results keyed by
// the explicit labels in its source block.
type NamedParallelExpr struct {
	branches []namedParallelBranch
}

func (*NamedParallelExpr) Node() {}

func (e *NamedParallelExpr) Children() []compiler.Expr {
	children := make([]compiler.Expr, len(e.branches))
	for i, branch := range e.branches {
		children[i] = branch.expr
	}
	return children
}

func (e *NamedParallelExpr) DescribeActionGraph() compiler.ActionGraphDescription {
	labels := make([]string, len(e.branches))
	for i, branch := range e.branches {
		labels[i] = branch.label
	}
	return compiler.ActionGraphDescription{
		Kind:        parallelKeyword,
		Name:        parallelKeyword,
		Form:        compiler.ActionGraphParallel,
		ChildLabels: labels,
	}
}

func (e *NamedParallelExpr) Analyze(resolver compiler.CapabilityResolver) error {
	for _, branch := range e.branches {
		if err := branch.expr.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (e *NamedParallelExpr) Build(ctx context.Context, bCtx *compiler.BuildContext) (action.AnyAction, error) {
	// Kernel returns partial results alongside a branch error; Flow's pipe semantics discard them, matching `(a & b)`.
	routes := make(map[string]action.AnyAction, len(e.branches))
	var children []action.IntrospectionChild
	if bCtx.Introspect {
		children = make([]action.IntrospectionChild, 0, len(e.branches))
	}
	for _, branch := range e.branches {
		compiled, err := branch.expr.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		routes[branch.label] = compiled
		if bCtx.Introspect {
			children = append(children, action.IntrospectionChild{
				Node: compiled, Relation: compiler.IntrospectionKindBranch, Role: "branch", Label: branch.label,
			})
		}
	}
	program := action.ParallelNamed[any](parallelKeyword, routes).Build()
	return compiler.TrackActionComposition(bCtx, program, action.IntrospectionComposition{
		Kind: "parallel.named", Children: children,
	}), nil
}
