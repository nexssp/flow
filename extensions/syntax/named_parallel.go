package syntax

import (
	"context"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

type namedParallelKeyword struct{}

func (namedParallelKeyword) Name() string    { return "named_parallel" }
func (namedParallelKeyword) Keyword() string { return "parallel" }

func (namedParallelKeyword) Parse(p *core.Parser) (core.Expr, error) {
	p.Advance() // consume "parallel"
	if p.Current().Type != core.TokLBrace {
		return nil, p.Fail(p.Current().Line, "expected '{' after parallel")
	}
	p.Advance()

	if p.Current().Type == core.TokRBrace {
		return nil, p.Fail(p.Current().Line, "parallel block must contain at least one branch")
	}

	branches := make([]namedParallelBranch, 0, 2)
	seen := make(map[string]struct{})
	for {
		labelToken := p.Current()
		if labelToken.Type != core.TokIdent {
			return nil, p.Fail(labelToken.Line, "expected parallel branch label")
		}
		label := labelToken.Lit
		if _, exists := seen[label]; exists {
			return nil, p.Fail(labelToken.Line, "duplicate parallel branch label %q", label)
		}
		seen[label] = struct{}{}
		p.Advance()

		if p.Current().Type != core.TokColon {
			return nil, p.Fail(p.Current().Line, "expected ':' after parallel branch label %q", label)
		}
		p.Advance()

		branchExpr, err := p.ParseExpr(0)
		if err != nil {
			return nil, err
		}
		branches = append(branches, namedParallelBranch{label: label, expr: branchExpr})

		switch p.Current().Type {
		case core.TokComma:
			p.Advance()
			if p.Current().Type == core.TokRBrace {
				p.Advance()
				return &NamedParallelExpr{branches: branches}, nil
			}
		case core.TokRBrace:
			p.Advance()
			return &NamedParallelExpr{branches: branches}, nil
		case core.TokEOF, core.TokIdent, core.TokString, core.TokNumber,
			core.TokArrow, core.TokPipe, core.TokAmpersand, core.TokOrOr,
			core.TokLParen, core.TokRParen, core.TokLBrace, core.TokColon,
			core.TokEquals, core.TokQuestion, core.TokHash, core.TokTilde,
			core.TokAtBrace, core.TokAtPrompt:
			return nil, p.Fail(p.Current().Line, "expected ',' or '}' after parallel branch %q", label)
		}
	}
}

type namedParallelBranch struct {
	label string
	expr  core.Expr
}

// NamedParallelExpr runs expressions concurrently and returns results keyed by
// the explicit labels in its source block.
type NamedParallelExpr struct {
	branches []namedParallelBranch
}

func (*NamedParallelExpr) Node() {}

func (e *NamedParallelExpr) Children() []core.Expr {
	children := make([]core.Expr, len(e.branches))
	for i, branch := range e.branches {
		children[i] = branch.expr
	}
	return children
}

func (e *NamedParallelExpr) Analyze(resolver core.CapabilityResolver) error {
	for _, branch := range e.branches {
		if err := branch.expr.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (e *NamedParallelExpr) Build(ctx context.Context, bCtx *core.BuildContext) (action.AnyAction, error) {
	// Kernel returns partial results alongside a branch error; Flow's pipe semantics discard them, matching `(a & b)`.
	routes := make(map[string]action.AnyAction, len(e.branches))
	for _, branch := range e.branches {
		compiled, err := branch.expr.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		routes[branch.label] = compiled
	}
	return action.ParallelNamed[any]("parallel", routes).Build(), nil
}
