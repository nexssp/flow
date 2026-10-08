package macros

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// newTestParser builds a parser with only the macroPrimary wired.
func newTestParser(tb testing.TB, byName map[string]Declaration, source string) *core.Parser {
	tb.Helper()
	return core.NewParserWithFileOffset(
		tb.Context(),
		core.NewOperatorTable(),
		core.NewPrimaryExtensionTable(&macroPrimary{byName: byName}),
		source,
		"<test>",
		0,
	)
}

func TestPrimary_Identity(t *testing.T) {
	t.Parallel()
	m := &macroPrimary{}
	ktest.RequireEqual(t, m.Name(), "macros.expand")
	ktest.RequireEqual(t, m.TokenType(), core.TokAtPrompt)
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
		{"missing arg", `$a -> $b`, []string{"a", "b"}, []string{"noop"}, `noop -> `},
		{"repeated", `$v + $v`, []string{"v"}, []string{"1"}, `1 + 1`},
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

	// The macro body is chosen per case: parameterless calls cannot
	// substitute $v (it would become `runtime.const @{ value:  }`, invalid
	// DSL) so they use a body that ignores the parameter.
	cases := []struct {
		name   string
		source string
		body   string
	}{
		{"no parens", `@echo`, `runtime.const @{ value: "default" }`},
		{"empty parens", `@echo()`, `runtime.const @{ value: "default" }`},
		{"single arg", `@echo("x")`, `runtime.const @{ value: $v }`},
		{"comma in quotes", `@echo("a,b")`, `runtime.const @{ value: $v }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			byName := map[string]Declaration{
				"echo": {Name: "echo", Params: []string{"v"}, Body: c.body},
			}
			expr, err := newTestParser(t, byName, c.source).Parse()
			ktest.RequireNoError(t, err)
			ktest.RequireCondition(t, expr != nil, "nil expr")
		})
	}
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
