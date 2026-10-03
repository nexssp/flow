package core

import (
	"context"
	"slices"
	"testing"

	"github.com/nexssp/kernel/action"
)

func testModifier(name string, inherit bool) Modifier {
	return Modifier{
		Name:        name,
		Inheritable: inherit,
		Apply: func(_ *action.Builder[any, any], _ string) error {
			return nil
		},
	}
}

func TestFilterInheritable_DropsDeclaredNonInheritable(t *testing.T) {
	table := NewModifierTable(
		testModifier("inherit", true),
		testModifier("meta", false),
	)
	fn := func(int) []string {
		return []string{"inherit=1", "meta=2", "unknown=3"}
	}
	got := filterInheritable(fn, table)(0)

	want := []string{"inherit=1", "unknown=3"}
	if !slices.Equal(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestFilterInheritable_NilTableReturnsFn(t *testing.T) {
	fn := func(int) []string { return []string{"x=1"} }
	got := filterInheritable(fn, nil)
	if got == nil {
		t.Fatal("nil table must return fn unchanged")
	}
	if out := got(0); !slices.Equal(out, []string{"x=1"}) {
		t.Fatalf("passthrough broken: %v", out)
	}
}

func TestPrependInherited_AtomWinsByName(t *testing.T) {
	parser := NewParserWithFileOffset(
		context.Background(),
		NewOperatorTable(),
		nil,
		"noop:timeout=5s",
		"",
		0,
	)
	parser.WithLineModifiers([]func(int) []string{
		func(int) []string { return []string{"retry=3", "timeout=1s"} },
	})

	ast, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := ast.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", ast)
	}

	want := []string{"retry=3", "timeout=5s"}
	if !slices.Equal(atom.Modifiers, want) {
		t.Fatalf("want %v, got %v", want, atom.Modifiers)
	}
}

func TestPrependInherited_LaterLookupWins(t *testing.T) {
	parser := NewParserWithFileOffset(
		context.Background(),
		NewOperatorTable(),
		nil,
		"noop",
		"",
		0,
	)
	parser.WithLineModifiers([]func(int) []string{
		func(int) []string { return []string{"timeout=1ms"} },
		func(int) []string { return []string{"timeout=1s"} },
	})

	ast, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := ast.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", ast)
	}

	want := []string{"timeout=1s"}
	if !slices.Equal(atom.Modifiers, want) {
		t.Fatalf("want %v, got %v", want, atom.Modifiers)
	}
}
