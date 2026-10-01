package core

import (
	"context"
	"testing"
)

func TestParseTernary_ThreeArm(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `a ? b : c`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cond, ok := expr.(*ConditionalExpr)
	if !ok {
		t.Fatalf("expected *ConditionalExpr, got %T", expr)
	}
	condAtom, ok := cond.Cond.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for Cond, got %T", cond.Cond)
	}
	if condAtom.Name != "a" {
		t.Errorf("cond = %v, want a", cond.Cond)
	}
	thenAtom, ok := cond.Then.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for Then, got %T", cond.Then)
	}
	if thenAtom.Name != "b" {
		t.Errorf("then = %v, want b", cond.Then)
	}
	if cond.Else == nil {
		t.Fatal("expected Else branch")
	}
	elseAtom, ok := cond.Else.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for Else, got %T", cond.Else)
	}
	if elseAtom.Name != "c" {
		t.Errorf("else = %v, want c", cond.Else)
	}
}

func TestParseTernary_TwoArm(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `a ? b`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cond, ok := expr.(*ConditionalExpr)
	if !ok {
		t.Fatalf("expected *ConditionalExpr, got %T", expr)
	}
	thenAtom, ok := cond.Then.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for Then, got %T", cond.Then)
	}
	if thenAtom.Name != "b" {
		t.Errorf("then = %v, want b", cond.Then)
	}
	if cond.Else != nil {
		t.Errorf("else = %v, want nil", cond.Else)
	}
}

func TestParseTernary_RightAssociative(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `a ? b : c ? d : e`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	outer, ok := expr.(*ConditionalExpr)
	if !ok {
		t.Fatalf("expected *ConditionalExpr, got %T", expr)
	}
	inner, ok := outer.Else.(*ConditionalExpr)
	if !ok {
		t.Fatalf("else = %T, want nested *ConditionalExpr", outer.Else)
	}
	innerCond, ok := inner.Cond.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for inner.Cond, got %T", inner.Cond)
	}
	innerThen, ok := inner.Then.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for inner.Then, got %T", inner.Then)
	}
	innerElse, ok := inner.Else.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for inner.Else, got %T", inner.Else)
	}
	if innerCond.Name != "c" || innerThen.Name != "d" || innerElse.Name != "e" {
		t.Errorf("inner = %+v, want c ? d : e", inner)
	}
}

func TestParseTernary_ModifierBeforeColon(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `a ? b:tag=fast : c`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cond, ok := expr.(*ConditionalExpr)
	if !ok {
		t.Fatalf("expected *ConditionalExpr, got %T", expr)
	}
	b, ok := cond.Then.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for cond.Then, got %T", cond.Then)
	}
	if b.Name != "b" || len(b.Modifiers) != 1 || b.Modifiers[0] != "tag=fast" {
		t.Errorf("then atom = %+v, want b:tag=fast", b)
	}
	elseAtom, ok := cond.Else.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for cond.Else, got %T", cond.Else)
	}
	if elseAtom.Name != "c" {
		t.Errorf("else = %v, want c", cond.Else)
	}
}

func TestParseTernary_InsideConditional(t *testing.T) {
	t.Parallel()
	ops := NewOperatorTable(Operator{
		Meta: OperatorMeta{
			Name:          "pipe",
			Token:         TokArrow,
			Precedence:    7,
			Associativity: Left,
		},
		Handler: func(_ context.Context, req OperatorReq) (OperatorRes, error) {
			return OperatorRes{Node: &PipeExpr{L: req.Left, R: req.Right}}, nil
		},
	})
	expr, err := NewParserWithPrimaries(context.Background(), ops, nil, `(a ? b : c) -> d`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	pipe, ok := expr.(*PipeExpr)
	if !ok {
		t.Fatalf("expected *PipeExpr, got %T", expr)
	}
	if _, isCond := pipe.L.(*ConditionalExpr); !isCond {
		t.Errorf("pipe.L = %T, want *ConditionalExpr", pipe.L)
	}
	rightAtom, ok := pipe.R.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom for pipe.R, got %T", pipe.R)
	}
	if rightAtom.Name != "d" {
		t.Errorf("pipe.R = %v, want d", pipe.R)
	}
}
