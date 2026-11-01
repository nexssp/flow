package macros

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

// newTestParser builds a parser with only the macroPrimary wired.
func newTestParser(tb testing.TB, byName map[string]Declaration, source string) *compiler.Parser {
	tb.Helper()
	return compiler.NewParserWithFileOffset(
		tb.Context(),
		compiler.NewOperatorTable(),
		compiler.NewPrimaryExtensionTable(&macroPrimary{byName: byName}),
		source,
		"<test>",
		0,
	)
}

func TestPrimary_Identity(t *testing.T) {
	t.Parallel()
	m := &macroPrimary{}
	ktest.RequireEqual(t, m.Name(), "macros.expand")
	ktest.RequireEqual(t, m.TokenType(), compiler.TokAtPrompt)
}

func TestPrimary_UnknownMacro(t *testing.T) {
	t.Parallel()
	_, err := newTestParser(t, map[string]Declaration{}, `@nope()`).Parse()
	ktest.RequireErrorContains(t, err, "unknown macro")
}

func TestSubstituteParams(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   string
		params []string
		args   []string
		want   string
	}{
		{"single", `runtime.const @{ value: $v }`, []string{"v"}, []string{`"hi"`}, `runtime.const @{ value: "hi" }`},
		{"multiple", `$a -> $b`, []string{"a", "b"}, []string{"runtime.noop", "runtime.debug"}, `runtime.noop -> runtime.debug`},
		{"missing arg", `$a -> $b`, []string{"a", "b"}, []string{"noop"}, `noop -> $b`},
		{"repeated", `$v + $v`, []string{"v"}, []string{"1"}, `1 + 1`},
		{"name boundary", `$v + $value`, []string{"v"}, []string{"1"}, `1 + $value`},
		{"no params", `noop`, nil, nil, `noop`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, substituteParams(c.body, c.params, c.args), c.want)
		})
	}
}

func TestReadArgs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		body   string
		params []string
	}{
		{"no parens", `@echo`, `runtime.const @{ value: "default" }`, nil},
		{"empty parens", `@echo()`, `runtime.const @{ value: "default" }`, nil},
		{"single arg", `@echo("x")`, `runtime.const @{ value: $v }`, []string{"v"}},
		{"comma in quotes", `@echo("a,b")`, `runtime.const @{ value: $v }`, []string{"v"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			byName := map[string]Declaration{
				"echo": {Name: "echo", Params: c.params, Body: c.body},
			}
			expr, err := newTestParser(t, byName, c.source).Parse()
			ktest.RequireNoError(t, err)
			ktest.RequireCondition(t, expr != nil, "nil expr")
		})
	}
}

func TestPrimaryQualifiedMacroCall(t *testing.T) {
	t.Parallel()
	byName := map[string]Declaration{
		"reporting.echo": {
			Name:   "echo",
			Params: []string{"value"},
			Body:   `runtime.const @{ value: $value }`,
		},
	}
	expr, err := newTestParser(t, byName, `reporting.@echo("qualified")`).Parse()
	ktest.RequireNoError(t, err)
	atom, ok := expr.(*compiler.Atom)
	ktest.RequireCondition(t, ok, "expected atom, got %T", expr)
	ktest.RequireEqual(t, atom.Args["value"].Str, "qualified")
}

func TestPrimaryMacroRequiresExactArity(t *testing.T) {
	t.Parallel()
	byName := map[string]Declaration{
		"echo": {Name: "echo", Params: []string{"value"}, Body: `runtime.noop`},
	}
	for _, tc := range []struct {
		source string
		want   string
	}{
		{source: `@echo()`, want: "received 0 arguments, want exactly 1"},
		{source: `@echo("one", "two")`, want: "received 2 arguments, want exactly 1"},
	} {
		_, err := newTestParser(t, byName, tc.source).Parse()
		ktest.RequireErrorContains(t, err, tc.want)
	}
}

func TestArgumentMapMacroSplicesEntries(t *testing.T) {
	t.Parallel()
	byName := map[string]Declaration{
		"stats_config": {
			Name:   "stats_config",
			Params: []string{"field"},
			Body:   "$field: @config.stats_$field",
		},
	}
	parsed, err := newTestParser(t, byName,
		`runtime.const @{ @stats_config(tree_only), @stats_config(full_tree) }`).Parse()
	ktest.RequireNoError(t, err)
	atom, ok := parsed.(*compiler.Atom)
	ktest.RequireCondition(t, ok, "expected atom, got %T", parsed)
	ktest.RequireEqual(t, atom.Args["tree_only"].Kind, compiler.ValueRef)
	ktest.RequireEqual(t, atom.Args["tree_only"].Ref, "config.stats_tree_only")
	ktest.RequireEqual(t, atom.Args["full_tree"].Ref, "config.stats_full_tree")
}

func TestArgumentMapQualifiedMacroSplicesEntries(t *testing.T) {
	t.Parallel()
	byName := map[string]Declaration{
		"reporting.stats_config": {
			Name:       "stats_config",
			Params:     []string{"field"},
			ParamKinds: []string{"Identifier"},
			Body:       `$field: 7`,
		},
	}
	parsed, err := newTestParser(t, byName, `runtime.const @{ reporting.@stats_config(tree_only) }`).Parse()
	ktest.RequireNoError(t, err)
	atom, ok := parsed.(*compiler.Atom)
	ktest.RequireCondition(t, ok, "expected atom, got %T", parsed)
	ktest.RequireEqual(t, atom.Args["tree_only"].Num, float64(7))
}

func TestArgumentMapMacroExpansionSkipsStringsAndComments(t *testing.T) {
	t.Parallel()
	byName := map[string]Declaration{
		"field": {Name: "field", Params: []string{"key"}, Body: "$key: 7"},
	}
	parsed, err := newTestParser(t, byName, "runtime.const @{\n"+
		`  text: "@field(hidden) # }",`+"\n"+
		"  # @field(commented) }\n"+
		"  @field(real)\n"+
		"}").Parse()
	ktest.RequireNoError(t, err)
	atom, ok := parsed.(*compiler.Atom)
	ktest.RequireCondition(t, ok, "expected atom, got %T", parsed)
	ktest.RequireEqual(t, atom.Args["text"].Str, "@field(hidden) # }")
	ktest.RequireEqual(t, atom.Args["real"].Num, float64(7))
	ktest.RequireEqual(t, len(atom.Args), 2)
}

func TestArgumentMapMacroDuplicateKeyStillFails(t *testing.T) {
	t.Parallel()
	byName := map[string]Declaration{
		"field": {Name: "field", Params: []string{"key"}, Body: "$key: 7"},
	}
	_, err := newTestParser(t, byName, `runtime.const @{ @field(foo), foo: 8 }`).Parse()
	ktest.RequireErrorContains(t, err, "duplicate key \"foo\"")
}

func TestArgumentMapMacroReportsUnknownInvocation(t *testing.T) {
	t.Parallel()
	_, err := newTestParser(t, nil, `runtime.const @{ @missing() }`).Parse()
	ktest.RequireErrorContains(t, err, "unknown macro @missing")
}

func TestPrimary_FlagRefIsNotReportedAsMacro(t *testing.T) {
	t.Parallel()
	_, err := newTestParser(t, map[string]Declaration{}, `@flag.endpoint`).Parse()
	ktest.RequireErrorContains(t, err, "flag reference, not a macro")
	ktest.RequireErrorNotContains(t, err, "unknown macro")
}

func TestPrimary_ConfigRefIsNotReportedAsMacro(t *testing.T) {
	t.Parallel()
	_, err := newTestParser(t, map[string]Declaration{}, `@config.db_url`).Parse()
	ktest.RequireErrorContains(t, err, "config reference, not a macro")
}
