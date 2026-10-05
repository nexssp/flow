package core

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/kernel/action"
)

type postfixTestExpr struct{ left Expr }

func (*postfixTestExpr) Node()              {}
func (e *postfixTestExpr) Children() []Expr { return []Expr{e.left} }
func (*postfixTestExpr) Analyze(CapabilityResolver) error {
	return nil
}

func (*postfixTestExpr) Build(context.Context, *BuildContext) (action.AnyAction, error) {
	return nil, nil
}

type postfixTestExtension struct{}

func (postfixTestExtension) Name() string { return "postfix-test" }
func (postfixTestExtension) Parse(*Parser) (Expr, error) {
	return nil, errors.New("postfix-test is not a primary")
}
func (postfixTestExtension) PostfixToken() TokenType { return TokQuestion }
func (postfixTestExtension) MatchesPostfix(p *Parser) bool {
	return p.Current().Type == TokQuestion &&
		p.Peek(1).Type == TokIdent && p.Peek(1).Lit == "on_error" &&
		p.Peek(2).Type == TokLBrace
}

func (postfixTestExtension) ParsePostfix(p *Parser, left Expr) (Expr, error) {
	p.Advance() // ?
	p.Advance() // on_error
	p.Advance() // {
	if p.Current().Type != TokRBrace {
		return nil, p.Fail(p.Current().Line, "test postfix expects an empty block")
	}
	p.Advance() // }
	return &postfixTestExpr{left: left}, nil
}

func TestPostfixExtensionDispatch(t *testing.T) {
	t.Parallel()

	primaries := NewPrimaryExtensionTable(postfixTestExtension{})
	parsed, err := NewParserWithPrimaries(
		context.Background(), NewOperatorTable(), primaries, `task.run ? on_error {}`,
	).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	guard, ok := parsed.(*postfixTestExpr)
	if !ok {
		t.Fatalf("parsed expression = %T, want *postfixTestExpr", parsed)
	}
	left, ok := guard.left.(*Atom)
	if !ok || left.Name != "task.run" {
		t.Fatalf("postfix left = %#v, want task.run atom", guard.left)
	}
}

func TestPostfixExtensionDoesNotStealTwoArmTernary(t *testing.T) {
	t.Parallel()

	primaries := NewPrimaryExtensionTable(postfixTestExtension{})
	parsed, err := NewParserWithPrimaries(
		context.Background(), NewOperatorTable(), primaries, `condition ? on_error`,
	).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := parsed.(*ConditionalExpr); !ok {
		t.Fatalf("parsed expression = %T, want *ConditionalExpr", parsed)
	}
}
