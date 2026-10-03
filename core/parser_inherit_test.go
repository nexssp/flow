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
	lk := LineLookup{
		Source: ModifierSource{Kind: "test"},
		Fn: func(int) []string {
			return []string{"inherit=1", "meta=2", "unknown=3"}
		},
	}
	got := filterInheritable(lk, table).Fn(0)

	want := []string{"inherit=1", "unknown=3"}
	if !slices.Equal(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestFilterInheritable_NilTableReturnsFn(t *testing.T) {
	lk := LineLookup{
		Source: ModifierSource{Kind: "test"},
		Fn:     func(int) []string { return []string{"x=1"} },
	}
	got := filterInheritable(lk, nil)
	if got.Fn == nil {
		t.Fatal("nil table must return lookup unchanged")
	}
	if out := got.Fn(0); !slices.Equal(out, []string{"x=1"}) {
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
	parser.WithLineModifiers(LineLookup{
		Source: ModifierSource{Kind: "test"},
		Fn:     func(int) []string { return []string{"retry=3", "timeout=1s"} },
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
	parser.WithLineModifiers(
		LineLookup{
			Source: ModifierSource{Kind: "outer"},
			Fn:     func(int) []string { return []string{"timeout=1ms"} },
		},
		LineLookup{
			Source: ModifierSource{Kind: "inner"},
			Fn:     func(int) []string { return []string{"timeout=1s"} },
		},
	)

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

func TestSubParse_InheritsLineModifiers(t *testing.T) {
	parser := NewParserWithFileOffset(
		context.Background(),
		NewOperatorTable(),
		nil,
		"noop",
		"",
		0,
	)
	parser.WithLineModifiers(LineLookup{
		Source: ModifierSource{Kind: "scope"},
		Fn:     func(int) []string { return []string{"timeout=5s"} },
	})

	sub, err := parser.SubParse("noop")
	if err != nil {
		t.Fatalf("sub-parse: %v", err)
	}
	atom, ok := sub.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", sub)
	}
	if len(atom.Modifiers) == 0 || atom.Modifiers[0] != "timeout=5s" {
		t.Fatalf("sub-parse did not inherit lineModifiers, got %v", atom.Modifiers)
	}
}
