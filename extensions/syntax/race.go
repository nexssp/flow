package syntax

import (
	"context"
	"fmt"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

const raceKeywordName = "race"

type raceKeyword struct{}

func (raceKeyword) Name() string    { return "race" }
func (raceKeyword) Keyword() string { return raceKeywordName }

func (raceKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	p.Advance() // consume "race"
	if p.Current().Type != compiler.TokLBrace {
		return nil, p.Fail(p.Current().Line, "expected '{' after race")
	}
	p.Advance()
	if p.Current().Type == compiler.TokRBrace {
		return nil, p.Fail(p.Current().Line, "race block must contain at least one candidate")
	}

	candidates := make([]compiler.Expr, 0, 2)
	for {
		candidate, err := p.ParseExpr(0)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)

		switch p.Current().Type {
		case compiler.TokComma:
			p.Advance()
			if p.Current().Type == compiler.TokRBrace {
				p.Advance()
				return &RaceExpr{candidates: candidates}, nil
			}
		case compiler.TokRBrace:
			p.Advance()
			return &RaceExpr{candidates: candidates}, nil
		case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
			compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr,
			compiler.TokLParen, compiler.TokRParen, compiler.TokLBrace, compiler.TokColon,
			compiler.TokEquals, compiler.TokQuestion, compiler.TokHash, compiler.TokTilde,
			compiler.TokAtBrace, compiler.TokAtPrompt:
			return nil, p.Fail(p.Current().Line, "expected ',' or '}' after race candidate")
		}
	}
}

// RaceExpr runs its candidates concurrently and returns Kernel's first
// successful result. Kernel cancels the remaining candidates on success and
// joins all launched work before returning.
type RaceExpr struct {
	candidates []compiler.Expr
}

func (*RaceExpr) Node() {}

func (e *RaceExpr) Children() []compiler.Expr {
	children := make([]compiler.Expr, len(e.candidates))
	copy(children, e.candidates)
	return children
}

func (e *RaceExpr) DescribeActionGraph() compiler.ActionGraphDescription {
	labels := make([]string, len(e.candidates))
	for i := range labels {
		labels[i] = fmt.Sprintf("candidate_%d", i+1)
	}
	return compiler.ActionGraphDescription{
		Kind:        raceKeywordName,
		Name:        raceKeywordName,
		Form:        compiler.ActionGraphParallel,
		ChildLabels: labels,
	}
}

func (e *RaceExpr) Analyze(resolver compiler.CapabilityResolver) error {
	for _, candidate := range e.candidates {
		if err := candidate.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (e *RaceExpr) Build(ctx context.Context, bCtx *compiler.BuildContext) (action.AnyAction, error) {
	candidates := make([]action.AnyAction, len(e.candidates))
	children := make([]action.IntrospectionChild, 0, len(e.candidates))
	for i, candidate := range e.candidates {
		compiled, err := candidate.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		candidates[i] = compiled
		if bCtx.Introspect {
			children = append(children, action.IntrospectionChild{
				Node: compiled, Relation: "candidate", Role: "candidate",
				Label: fmt.Sprintf("candidate_%d", i+1),
			})
		}
	}

	program := action.RaceAny(raceKeywordName, candidates...).Build()
	return compiler.TrackActionComposition(bCtx, program, action.IntrospectionComposition{
		Kind: raceKeywordName, Children: children,
	}), nil
}
