package core

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
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

func TestParse_PostfixModifiersOnProjection(t *testing.T) {
	ops := NewOperatorTable(testPipe())
	primaries := NewPrimaryExtensionTable(testProjectionPrimary{})
	src := `{ value: "done" }:cost_estimate=100000 -> noop`
	expr, err := NewParserWithFileOffset(
		context.Background(), ops, primaries, src, "<test>", 0,
	).Parse()
	ktest.RequireNoError(t, err)

	pipe, ok := expr.(*PipeExpr)
	ktest.RequireCondition(t, ok, "got %T, want *PipeExpr", expr)
	decorated, ok := pipe.L.(*DecoratedExpr)
	ktest.RequireCondition(t, ok, "got %T, want *DecoratedExpr", pipe.L)
	ktest.RequireEqual(t, decorated.Modifiers, []string{"cost_estimate=100000"})
	if _, isProjection := decorated.Inner.(*ProjectionExpr); !isProjection {
		t.Fatalf("Inner = %T, want *ProjectionExpr", decorated.Inner)
	}
}

func TestParse_PostfixModifiersOnLoop(t *testing.T) {
	ops := NewOperatorTable(testPipe())
	primaries := NewPrimaryExtensionTable(testLoopPrimary{})
	src := `loop(noop):retry=2`
	expr, err := NewParserWithFileOffset(
		context.Background(), ops, primaries, src, "<test>", 0,
	).Parse()
	ktest.RequireNoError(t, err)

	decorated, ok := expr.(*DecoratedExpr)
	ktest.RequireCondition(t, ok, "got %T, want *DecoratedExpr", expr)
	ktest.RequireEqual(t, decorated.Modifiers, []string{"retry=2"})
	if _, isLoop := decorated.Inner.(*testLoopExpr); !isLoop {
		t.Fatalf("Inner = %T, want *testLoopExpr", decorated.Inner)
	}
}

func TestParse_PostfixModifiersOnAtom(t *testing.T) {
	ops := NewOperatorTable(testPipe())
	primaries := NewPrimaryExtensionTable()
	src := `noop:timeout=5s:retry=3`
	expr, err := NewParserWithFileOffset(
		context.Background(), ops, primaries, src, "<test>", 0,
	).Parse()
	ktest.RequireNoError(t, err)

	atom, ok := expr.(*Atom)
	ktest.RequireCondition(t, ok, "got %T, want *Atom (not DecoratedExpr)", expr)
	ktest.RequireEqual(t, atom.Modifiers, []string{"timeout=5s", "retry=3"})
}

// testLoopExpr is a minimal *LoopExpr shim used by parser tests to
// exercise postfix-modifier absorption on a keyword primary without
// importing extensions/loop (which would create an import cycle).
type testLoopExpr struct{ Body Expr }

func (*testLoopExpr) Node()                            {}
func (l *testLoopExpr) Children() []Expr               { return []Expr{l.Body} }
func (*testLoopExpr) Analyze(CapabilityResolver) error { return nil }
func (*testLoopExpr) Build(context.Context, *BuildContext) (action.AnyAction, error) {
	return nil, nil
}

// testLoopPrimary is a minimal keyword primary that consumes
// `loop(EXPR)` and returns a *testLoopExpr.
type testLoopPrimary struct{}

func (testLoopPrimary) Name() string    { return "test_loop" }
func (testLoopPrimary) Keyword() string { return "loop" }
func (testLoopPrimary) Parse(p *Parser) (Expr, error) {
	p.Advance() // consume 'loop'
	if p.Current().Type != TokLParen {
		return nil, p.Fail(p.Current().Line, "expected '(' after loop")
	}
	p.Advance()
	body, err := p.ParseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.Current().Type != TokRParen {
		return nil, p.Fail(p.Current().Line, "expected ')' after loop body")
	}
	p.Advance()
	return &testLoopExpr{Body: body}, nil
}

type testProjectionPrimary struct{}

func (testProjectionPrimary) Name() string         { return "test_projection" }
func (testProjectionPrimary) TokenType() TokenType { return TokLBrace }
func (testProjectionPrimary) Parse(p *Parser) (Expr, error) {
	p.Advance() // consume '{'
	start := p.Current().Offset
	depth := 1
	for p.Current().Type != TokEOF {
		if p.Current().Type == TokLBrace {
			depth++
		} else if p.Current().Type == TokRBrace {
			depth--
			if depth == 0 {
				break
			}
		}
		p.Advance()
	}
	if p.Current().Type != TokRBrace {
		return nil, p.Fail(p.Current().Line, "unclosed projection")
	}
	raw := p.Src()[start:p.Current().Offset]
	p.Advance()
	return &ProjectionExpr{Raw: raw}, nil
}
