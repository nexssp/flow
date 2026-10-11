package on_error

import (
	"context"
	"errors"
	"fmt"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	core "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/match"
)

type ErrorGuardCase struct {
	Kind   xerr.Kind
	IsElse bool
	Body   core.Expr
}

type ErrorGuardExpr struct {
	Protected core.Expr
	Cases     []ErrorGuardCase
}

func (*ErrorGuardExpr) Node() {}

func (g *ErrorGuardExpr) Children() []core.Expr {
	children := make([]core.Expr, 0, len(g.Cases)+1)
	children = append(children, g.Protected)
	for i := range g.Cases {
		children = append(children, g.Cases[i].Body)
	}
	return children
}

func (g *ErrorGuardExpr) DescribeActionGraph() core.ActionGraphDescription {
	labels := make([]string, len(g.Cases)+1)
	labels[0] = "protected"
	for i, current := range g.Cases {
		labels[i+1] = "on " + string(current.Kind)
		if current.IsElse {
			labels[i+1] = "else"
		}
	}
	return core.ActionGraphDescription{
		Kind:        "error.guard",
		Name:        "on_error.guard",
		Form:        core.ActionGraphCatch,
		ChildLabels: labels,
	}
}

func (g *ErrorGuardExpr) Analyze(resolver core.CapabilityResolver) error {
	if g.Protected == nil {
		return xerr.Validation("on_error: missing protected expression")
	}
	if err := g.Protected.Analyze(resolver); err != nil {
		return err
	}
	for i := range g.Cases {
		if g.Cases[i].Body == nil {
			return xerr.Validation("on_error: missing recovery expression")
		}
		if err := g.Cases[i].Body.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (g *ErrorGuardExpr) Build(ctx context.Context, buildCtx *core.BuildContext) (action.AnyAction, error) {
	if g.Protected == nil {
		return nil, xerr.Validation("on_error: missing protected expression")
	}
	protected, err := g.Protected.Build(ctx, buildCtx)
	if err != nil {
		return nil, err
	}

	conditionCases := make([]match.ConditionCase, len(g.Cases))
	recoveryActions := make([]action.AnyAction, len(g.Cases))
	var children []action.IntrospectionChild
	if buildCtx.Introspect {
		children = make([]action.IntrospectionChild, 0, len(g.Cases)+1)
		children = append(children, action.IntrospectionChild{
			Node: protected, Relation: "protects", Role: "protected",
		})
	}
	seenKinds := make(map[xerr.Kind]bool, len(g.Cases))
	seenElse := false
	for i, current := range g.Cases {
		if current.Body == nil {
			return nil, xerr.Validation("on_error: missing recovery expression")
		}
		if current.IsElse {
			if seenElse || i != len(g.Cases)-1 {
				return nil, xerr.Validation("on_error: else case must appear once and last")
			}
			seenElse = true
			conditionCases[i].IsDefault = true
		} else {
			if _, ok := match.ResolveKindSymbol(match.KindSymbol(current.Kind)); !ok || seenKinds[current.Kind] {
				return nil, xerr.Validation("on_error: invalid or duplicate Kernel kind " + string(current.Kind))
			}
			seenKinds[current.Kind] = true
			program, compileErr := match.CompileCondition("error.kind == " + match.KindSymbol(current.Kind))
			if compileErr != nil {
				return nil, xerr.Validation("on_error: invalid Kernel kind condition", compileErr)
			}
			conditionCases[i].Program = program
		}

		recoveryActions[i], err = current.Body.Build(ctx, buildCtx)
		if err != nil {
			return nil, err
		}
		if buildCtx.Introspect {
			label := string(current.Kind)
			if current.IsElse {
				label = "else"
			}
			children = append(children, action.IntrospectionChild{
				Node: recoveryActions[i], Relation: "recovers_to", Role: "handler", Label: label,
			})
		}
	}

	program := action.CatchAny("on_error.guard", protected, func(execCtx context.Context, request any, primaryErr error) (any, error) {
		if terminalErr := terminalRecoveryError(execCtx, primaryErr); terminalErr != nil {
			return nil, terminalErr
		}

		env := recoveryEnvironment(request, primaryErr)
		caseIndex := match.FirstMatchingCase(conditionCases, env)
		if caseIndex < 0 {
			return nil, primaryErr
		}

		result, handlerErr := action.InvokeAny(execCtx, recoveryActions[caseIndex], env)
		if handlerErr != nil {
			both := errors.Join(primaryErr, handlerErr)
			return nil, fmt.Errorf("on_error recovery handler failed: %w", both)
		}
		return result, nil
	}).Tag("error", "recovery").Build()
	composition := action.IntrospectionComposition{Kind: "error_recovery", Children: children}
	if buildCtx.Introspect {
		composition.Opaque = true
		composition.Notes = []string{"handler selection predicate is closure-captured and omitted"}
	}
	return core.TrackActionComposition(buildCtx, program, composition), nil
}

// primaryOnlyPrecedence is higher than every DSL binary operator
// precedence (-> = 7, | = 7, & = 5, || = 3, ? = 1). Parsing at this
// precedence consumes exactly one primary expression and no operator
// continuation, which is what the short form needs so it does not
// swallow a following `->` or `|`.
//
// NEW: introduced for the short-form `? on_error target` syntax.
const primaryOnlyPrecedence = 100

type errorGuardPrimary struct{}

func (errorGuardPrimary) Name() string { return "on_error.guard" }
func (errorGuardPrimary) Parse(p *core.Parser) (core.Expr, error) {
	return nil, p.Fail(p.Current().Line, "on_error guard must follow an expression")
}
func (errorGuardPrimary) PostfixToken() core.TokenType { return core.TokQuestion }

// CHANGED: previously required a `{` at Peek(2) so only the block
// form matched. The short form `? on_error target` is now also
// accepted; the short/long decision is made in ParsePostfix.
func (errorGuardPrimary) MatchesPostfix(p *core.Parser) bool {
	return p.Current().Type == core.TokQuestion &&
		p.Peek(1).Type == core.TokIdent && p.Peek(1).Lit == "on_error"
}

func (errorGuardPrimary) ParsePostfix(p *core.Parser, protected core.Expr) (core.Expr, error) {
	p.Advance() // ?
	p.Advance() // on_error

	// NEW: short form — `expr ? on_error target`.
	//
	// When the token after `on_error` is not `{`, parse a single
	// primary expression at primaryOnlyPrecedence and treat it as the
	// catch-all recovery arm. This is exactly equivalent to
	//
	//     expr ? on_error { else -> target }
	//
	// but reads better when there is only one recovery and no kind
	// discrimination. The handler still receives the error environment,
	// so `error.passthrough` is the recommended catch-all when the
	// original input should be returned unchanged.
	if p.Current().Type != core.TokLBrace {
		body, err := p.ParseExpr(primaryOnlyPrecedence)
		if err != nil {
			return nil, err
		}
		return &ErrorGuardExpr{
			Protected: protected,
			Cases:     []ErrorGuardCase{{IsElse: true, Body: body}},
		}, nil
	}

	p.Advance() // {

	var cases []ErrorGuardCase
	seenKinds := make(map[xerr.Kind]bool)
	seenElse := false
	for p.Current().Type != core.TokRBrace && p.Current().Type != core.TokEOF {
		parsedCase, err := parseErrorGuardCase(p, seenKinds, &seenElse)
		if err != nil {
			return nil, err
		}
		cases = append(cases, parsedCase)

		if p.Current().Type == core.TokComma {
			p.Advance()
			if p.Current().Type == core.TokRBrace {
				break
			}
			if seenElse {
				return nil, p.Fail(p.Current().Line, "on_error else case must be last")
			}
			continue
		}
		if p.Current().Type != core.TokRBrace {
			return nil, p.Fail(p.Current().Line, "expected ',' or '}' after on_error case")
		}
	}

	if p.Current().Type != core.TokRBrace {
		return nil, p.Fail(p.Current().Line, "expected '}' closing on_error guard")
	}
	if len(cases) == 0 {
		return nil, p.Fail(p.Current().Line, "on_error guard requires at least one case")
	}
	p.Advance()
	return &ErrorGuardExpr{Protected: protected, Cases: cases}, nil
}

func parseErrorGuardCase(
	p *core.Parser,
	seenKinds map[xerr.Kind]bool,
	seenElse *bool,
) (ErrorGuardCase, error) {
	current := p.Current()
	if current.Type != core.TokIdent {
		return ErrorGuardCase{}, p.Fail(current.Line, "expected xerr.Kind symbol or else in on_error case")
	}

	parsedCase := ErrorGuardCase{}
	if current.Lit == "else" {
		if *seenElse {
			return ErrorGuardCase{}, p.Fail(current.Line, "on_error else case may appear only once")
		}
		*seenElse = true
		parsedCase.IsElse = true
	} else {
		kind, ok := match.ResolveKindSymbol(current.Lit)
		if !ok {
			return ErrorGuardCase{}, p.Fail(current.Line, "unknown Kernel kind symbol %q", current.Lit)
		}
		if seenKinds[kind] {
			return ErrorGuardCase{}, p.Fail(current.Line, "duplicate Kernel kind case %q", current.Lit)
		}
		seenKinds[kind] = true
		parsedCase.Kind = kind
	}
	p.Advance()
	if p.Current().Type != core.TokArrow {
		return ErrorGuardCase{}, p.Fail(p.Current().Line, "expected '->' after on_error case")
	}
	p.Advance()
	body, err := p.ParseExpr(0)
	if err != nil {
		return ErrorGuardCase{}, err
	}
	parsedCase.Body = body
	return parsedCase, nil
}
