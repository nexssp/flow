package core

import (
	"context"
	"testing"
)

func newTestParser(t testing.TB, src string) *Parser {
	t.Helper()
	return NewParser(context.Background(), NewOperatorTable(), src)
}

func TestParseAtBrace_ScalarValues(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap @{ key: "outer", count: 3, enabled: true, empty: null }`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	if len(atom.Args) != 4 {
		t.Fatalf("args count = %d, want 4", len(atom.Args))
	}
	if v := atom.Args["key"]; v.Kind != ValueString || v.Str != "outer" {
		t.Errorf("key = %+v, want string outer", v)
	}
	if v := atom.Args["count"]; v.Kind != ValueNumber || v.Num != 3 {
		t.Errorf("count = %+v, want number 3", v)
	}
	if v := atom.Args["enabled"]; v.Kind != ValueBool || !v.Bool {
		t.Errorf("enabled = %+v, want true", v)
	}
	if v := atom.Args["empty"]; v.Kind != ValueNull {
		t.Errorf("empty = %+v, want null", v)
	}
}

func TestParseAtBrace_NestedObjectAndSlice(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap @{ meta: { retry: 3, tags: ["a", "b"] } }`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	meta := atom.Args["meta"]
	if meta.Kind != ValueMap || len(meta.Map) != 2 {
		t.Fatalf("meta = %+v, want map with 2 entries", meta)
	}
	var retry, tags *Value
	for _, e := range meta.Map {
		switch e.Key {
		case "retry":
			retry = e.Value
		case "tags":
			tags = e.Value
		}
	}
	if retry == nil || retry.Kind != ValueNumber || retry.Num != 3 {
		t.Errorf("retry = %+v, want number 3", retry)
	}
	if tags == nil || tags.Kind != ValueSlice || len(tags.Slice) != 2 {
		t.Errorf("tags = %+v, want slice of 2", tags)
	}
}

func TestParseAtBrace_References(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap @{ from: .user.name, whole: . }`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	args := atom.Args
	if v := args["from"]; v.Kind != ValueRef || v.Ref != "user.name" {
		t.Errorf("from = %+v, want ref user.name", v)
	}
	if v := args["whole"]; !v.IsWholeStateRef() {
		t.Errorf("whole = %+v, want whole-state ref", v)
	}
}

func TestParseAtBrace_Empty(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap @{ }`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	if n := len(atom.Args); n != 0 {
		t.Errorf("args = %d, want 0", n)
	}
}

func TestParseAtBrace_AfterModifier(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap:timeout=5s @{ key: "outer" }`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	if len(atom.Modifiers) != 1 || atom.Modifiers[0] != "timeout=5s" {
		t.Errorf("modifiers = %v, want [timeout=5s]", atom.Modifiers)
	}
	if v := atom.Args["key"]; v == nil || v.Str != "outer" {
		t.Errorf("key = %+v, want outer", v)
	}
}

func TestParseAtBrace_RejectsMalformed(t *testing.T) {
	t.Parallel()
	cases := []string{
		`wrap @{ key }`,
		`wrap @{ key: }`,
		`wrap @{ : "v" }`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			if _, err := newTestParser(t, src).Parse(); err == nil {
				t.Fatalf("expected error for %q", src)
			}
		})
	}
}
