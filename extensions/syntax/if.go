package syntax

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

const ifKeywordName = "if"

type ifKeyword struct{}

func (ifKeyword) Name() string    { return ifKeywordName }
func (ifKeyword) Keyword() string { return ifKeywordName }

func (ifKeyword) Parse(p *compiler.Parser) (compiler.Expr, error) {
	position := p.Position()
	p.Advance() // consume `if`
	if p.Current().Type != compiler.TokLParen {
		return nil, p.Fail(p.Current().Line,
			"if is reserved for conditional expressions; use `if (condition) { ... }` (bare action or identifier `if` is not supported)")
	}

	condition, conditionPos, err := parseIfCondition(p)
	if err != nil {
		return nil, err
	}
	thenBranch, err := parseIfBlock(p, "if")
	if err != nil {
		return nil, err
	}

	var elseBranch compiler.Expr
	if p.Current().Type == compiler.TokIdent && p.Current().Lit == "else" {
		p.Advance()
		if p.Current().Type == compiler.TokIdent && p.Current().Lit == ifKeywordName {
			elseBranch, err = (ifKeyword{}).Parse(p)
		} else {
			elseBranch, err = parseIfBlock(p, "else")
		}
		if err != nil {
			return nil, err
		}
		if p.Current().Type == compiler.TokIdent && p.Current().Lit == "else" {
			return nil, p.Fail(p.Current().Line, "if: duplicate else clause")
		}
	}

	return &IfExpr{
		Condition: condition, ConditionPos: conditionPos,
		Then: thenBranch, Else: elseBranch, Pos: position,
	}, nil
}

func parseIfCondition(p *compiler.Parser) (string, compiler.Position, error) {
	if p.Current().Type != compiler.TokLParen {
		return "", compiler.Position{}, p.Fail(p.Current().Line, "if: expected '(' before condition")
	}
	p.Advance()
	conditionPos := p.Position()
	start := p.Current().Offset
	depth := 1
	for {
		token := p.Current()
		switch token.Type {
		case compiler.TokEOF:
			return "", compiler.Position{}, p.Fail(token.Line, "if: unclosed condition; expected ')'")
		case compiler.TokInvalid:
			if !isIfExpressionOperator(token.Lit) {
				return "", compiler.Position{}, p.Fail(token.Line, "unexpected character %q", token.Lit)
			}
		case compiler.TokLParen:
			depth++
		case compiler.TokRParen:
			depth--
			if depth == 0 {
				condition := strings.TrimSpace(p.Src()[start:token.Offset])
				if condition == "" {
					return "", compiler.Position{}, p.Fail(token.Line, "if: condition must not be empty")
				}
				p.Advance()
				return condition, conditionPos, nil
			}
		case compiler.TokIdent, compiler.TokString, compiler.TokNumber, compiler.TokArrow,
			compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr, compiler.TokLBrace,
			compiler.TokRBrace, compiler.TokLBracket, compiler.TokRBracket, compiler.TokColon,
			compiler.TokEquals, compiler.TokComma,
			compiler.TokQuestion, compiler.TokHash, compiler.TokTilde, compiler.TokAtBrace,
			compiler.TokAtPrompt:
			// Expression operators and operands are validated after extracting the raw condition.
		}
		p.Advance()
	}
}

func isIfExpressionOperator(literal string) bool {
	// The Flow lexer does not tokenize these expr-lang operators itself.
	switch literal {
	case "+", "-", "*", "/", "%", "<", ">", "^":
		return true
	default:
		return false
	}
}

func parseIfBlock(p *compiler.Parser, clause string) (compiler.Expr, error) {
	if p.Current().Type != compiler.TokLBrace {
		return nil, p.Fail(p.Current().Line, "if: expected '{' after %s", clause)
	}
	p.Advance()
	if p.Current().Type == compiler.TokRBrace {
		return nil, p.Fail(p.Current().Line, "if: %s block must contain a Flow expression", clause)
	}

	branch, err := p.ParseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.Current().Type != compiler.TokRBrace {
		if p.Current().Type == compiler.TokInvalid {
			return nil, p.Fail(p.Current().Line, "unexpected character %q", p.Current().Lit)
		}
		return nil, p.Fail(p.Current().Line, "if: expected '}' closing %s block", clause)
	}
	p.Advance()
	return branch, nil
}

// IfExpr evaluates its condition as a strict bool and runs only the selected
// Flow branch. An absent else branch returns the current input unchanged.
type IfExpr struct {
	Condition    string
	ConditionPos compiler.Position
	Then         compiler.Expr
	Else         compiler.Expr
	Pos          compiler.Position
}

func (*IfExpr) Node() {}

// Position exposes the if keyword's source location to compiler plans.
func (e *IfExpr) Position() compiler.Position { return e.Pos }

func (e *IfExpr) conditionPosition() compiler.Position {
	if e.ConditionPos != (compiler.Position{}) {
		return e.ConditionPos
	}
	return e.Pos
}

func (e *IfExpr) Children() []compiler.Expr {
	if e.Else == nil {
		return []compiler.Expr{e.Then}
	}
	return []compiler.Expr{e.Then, e.Else}
}

// RewriteCompileTimeExpressions updates the condition checked during analysis and execution.
func (e *IfExpr) RewriteCompileTimeExpressions(rewrite func(string, compiler.Position) (string, error)) error {
	condition, err := rewrite(e.Condition, e.conditionPosition())
	if err != nil {
		return err
	}
	e.Condition = condition
	return nil
}

func (e *IfExpr) DescribeActionGraph() compiler.ActionGraphDescription {
	description := compiler.ActionGraphDescription{
		Kind:        ifKeywordName,
		Name:        "if.evaluate",
		Form:        compiler.ActionGraphChoice,
		ChildLabels: []string{"true"},
	}
	if e.Else != nil {
		description.ChildLabels = append(description.ChildLabels, "false")
	} else {
		description.PassThroughLabels = []string{"false"}
	}
	return description
}

func (e *IfExpr) Analyze(resolver compiler.CapabilityResolver) error {
	if err := e.Then.Analyze(resolver); err != nil {
		return err
	}
	if e.Else != nil {
		return e.Else.Analyze(resolver)
	}
	return nil
}

func (e *IfExpr) AnalyzeTypes(ctx compiler.TypeAnalysisContext) (compiler.TypeDescriptor, error) {
	if ctx.CheckExpression != nil {
		condition, err := ctx.CheckExpression(compiler.StripExprComments(e.Condition), ctx, e.conditionPosition())
		if err != nil {
			return compiler.TypeDescriptor{}, err
		}
		if condition.Type != nil && condition.Type != reflect.TypeFor[bool]() && condition.Type.Kind() != reflect.Interface {
			return compiler.TypeDescriptor{}, compiler.SourceError(e.conditionPosition(),
				"if: condition must be bool, got %s", condition.Type)
		}
	}

	thenType, err := compiler.AnalyzeTypedExpression(ctx, e.Then)
	if err != nil {
		return compiler.TypeDescriptor{}, err
	}
	elseType := ctx.Input
	if e.Else != nil {
		elseType, err = compiler.AnalyzeTypedExpression(ctx, e.Else)
		if err != nil {
			return compiler.TypeDescriptor{}, err
		}
	}
	if thenType == elseType {
		return thenType, nil
	}
	return compiler.TypeDescriptor{}, nil
}

func (e *IfExpr) Build(ctx context.Context, bCtx *compiler.BuildContext) (action.AnyAction, error) {
	condition, err := bCtx.CompileExpression(compiler.StripExprComments(e.Condition), e.conditionPosition())
	if err != nil {
		return nil, err
	}

	thenBranch, err := e.Then.Build(ctx, bCtx)
	if err != nil {
		return nil, err
	}
	thenBranch = compiler.WithChildExpressionScopeAction(thenBranch)

	var elseBranch action.AnyAction
	if e.Else != nil {
		elseBranch, err = e.Else.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}
		elseBranch = compiler.WithChildExpressionScopeAction(elseBranch)
	}

	program := action.New("if.evaluate", func(execCtx context.Context, input any) (any, error) {
		if !compiler.HasExpressionScope(execCtx) {
			execCtx = compiler.WithExpressionScope(execCtx, input)
		}
		value, evalErr := condition.Evaluate(execCtx, input)
		if evalErr != nil {
			return nil, xerr.Validation("if: condition evaluation failed", evalErr)
		}
		matched, ok := value.(bool)
		if !ok {
			return nil, xerr.Validation(fmt.Sprintf("if: condition must return bool, got %T", value))
		}
		if matched {
			return action.InvokeAny(execCtx, thenBranch, input)
		}
		if elseBranch == nil {
			return input, nil
		}
		return action.InvokeAny(execCtx, elseBranch, input)
	}).Tag("control", "if").Build()

	composition := action.IntrospectionComposition{Kind: ifKeywordName}
	if bCtx.Introspect {
		composition.Children = []action.IntrospectionChild{{
			Node: thenBranch, Relation: "branch", Role: "true", Label: "true",
		}}
		if elseBranch != nil {
			composition.Children = append(composition.Children, action.IntrospectionChild{
				Node: elseBranch, Relation: "branch", Role: "false", Label: "false",
			})
		} else {
			composition.Notes = []string{"false condition returns the current input unchanged"}
		}
		composition.Opaque = true
		composition.Notes = append(composition.Notes, "condition expression is closure-captured")
	}
	return compiler.TrackActionComposition(bCtx, program, composition), nil
}
