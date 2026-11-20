package compiler

import (
	"context"
	"testing"
)

func parseStringEscapeSource(t *testing.T, source string) (Expr, error) {
	t.Helper()
	return NewParser(context.Background(), NewOperatorTable(), source).Parse()
}

func TestParseDoubleQuotedStringLiteralsUseExprGrammar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		source string
		want   string
	}{
		{name: "newline escape", source: `"line\nbreak"`, want: "line\nbreak"},
		{name: "braced Unicode escape", source: `"\u{41}"`, want: "A"},
		{name: "byte-like escape is a Unicode code point", source: `"\xff"`, want: "ÿ"},
		{name: "backtick remains raw", source: "`line\\nbreak`", want: `line\nbreak`},
		{name: "single quote retains legacy literal behavior", source: `'line\nbreak'`, want: `line\nbreak`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expr, err := parseStringEscapeSource(t, tc.source)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.source, err)
			}
			literal, ok := expr.(*LiteralExpr)
			if !ok || literal.Val != tc.want {
				t.Fatalf("expression = %#v, want string %q", expr, tc.want)
			}
		})
	}
}

func TestParseActionArgumentStringEscapes(t *testing.T) {
	t.Parallel()
	const source = "runtime.noop(value: \"line\\nbreak\", nested: {text: \"\\u{41}\"}, list: [\"\\xff\", `line\\nbreak`, 'line\\nbreak'])"
	expr, err := parseStringEscapeSource(t, source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expression = %T, want *Atom", expr)
	}
	if got := atom.Args["value"]; got == nil || got.Kind != ValueString || got.Str != "line\nbreak" {
		t.Fatalf("value = %+v, want decoded newline string", got)
	}
	nested := atom.Args["nested"]
	if nested == nil || nested.Kind != ValueMap || len(nested.Map) != 1 || nested.Map[0].Value.Str != "A" {
		t.Fatalf("nested = %+v, want decoded braced Unicode string", nested)
	}
	list := atom.Args["list"]
	if list == nil || list.Kind != ValueSlice || len(list.Slice) != 3 {
		t.Fatalf("list = %+v, want three strings", list)
	}
	if list.Slice[0].Str != "ÿ" || list.Slice[1].Str != `line\nbreak` || list.Slice[2].Str != `line\nbreak` {
		t.Fatalf("list = %+v, want byte-like escape decoded, raw backtick, and legacy single quote", list.Slice)
	}
}

func TestParseLegacyArgumentsAndRuntimeModifiersDecodeDoubleQuotedStrings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		source string
		check  func(*testing.T, Expr)
	}{
		{
			name:   "legacy key=value argument",
			source: `runtime.noop(value="line\nbreak")`,
			check: func(t *testing.T, expr Expr) {
				atom, ok := expr.(*Atom)
				if !ok || atom.Params["value"] != "line\nbreak" {
					t.Fatalf("expression = %#v, want decoded legacy value", expr)
				}
			},
		},
		{
			name:   "atom modifier",
			source: `runtime.noop :tag="line\nbreak"`,
			check: func(t *testing.T, expr Expr) {
				atom, ok := expr.(*Atom)
				if !ok || len(atom.Modifiers) != 1 || atom.Modifiers[0] != "tag=line\nbreak" || len(atom.ModifierValues) != 1 || !atom.ModifierValues[0].Quoted {
					t.Fatalf("expression = %#v, want decoded quoted modifier", expr)
				}
			},
		},
		{
			name:   "postfix modifier",
			source: `("value"):tag="line\nbreak"`,
			check: func(t *testing.T, expr Expr) {
				decorated, ok := expr.(*DecoratedExpr)
				if !ok || len(decorated.ModifierValues) != 1 || decorated.ModifierValues[0].Raw != "tag=line\nbreak" {
					t.Fatalf("expression = %#v, want decoded postfix modifier", expr)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expr, err := parseStringEscapeSource(t, tc.source)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.source, err)
			}
			tc.check(t, expr)
		})
	}
}

func TestParseRejectsMalformedDoubleQuotedRuntimeStrings(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`"bad\q"`,
		`runtime.noop(value: "bad\q")`,
		`runtime.noop(value="bad\q")`,
		`runtime.noop :tag="bad\q"`,
		"\"line\nbreak\"",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := parseStringEscapeSource(t, source); err == nil {
				t.Fatalf("parse(%q) succeeded, want invalid string error", source)
			}
		})
	}
}
