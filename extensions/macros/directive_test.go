package macros

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// runDirective drives handleDirective with a single line, returning
// the mutated Out map.
func runDirective(tb testing.TB, lines ...string) (map[string]any, core.DirectiveRes, error) {
	tb.Helper()
	out := map[string]any{}
	res, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	return out, res, err
}

func TestDirective_InlineBody(t *testing.T) {
	t.Parallel()
	out, res, err := runDirective(t, `@macro hi() { const @{ value: "hi" } }`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res.Next, 1)

	decls, _ := out[DeclarationKey].([]Declaration)
	ktest.RequireEqual(t, len(decls), 1)
	ktest.RequireEqual(t, decls[0].Name, "hi")
	ktest.RequireEqual(t, decls[0].Body, `const @{ value: "hi" }`)
	ktest.RequireEqual(t, len(decls[0].Params), 0)
}

func TestDirective_MultiLineBody(t *testing.T) {
	t.Parallel()
	lines := []string{
		`@macro echo(v) {`,
		`  const @{ value: $v }`,
		`}`,
	}
	out, res, err := runDirective(t, lines...)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res.Next, 3)

	decls, _ := out[DeclarationKey].([]Declaration)
	ktest.RequireEqual(t, len(decls), 1)
	ktest.RequireEqual(t, decls[0].Name, "echo")
	ktest.RequireEqual(t, decls[0].Params, []string{"v"})
	ktest.RequireEqual(t, decls[0].Body, `const @{ value: $v }`)
}

func TestDirective_MultipleParams(t *testing.T) {
	t.Parallel()
	out, _, err := runDirective(t, `@macro pair(k, v) { $k -> $v }`)
	ktest.RequireNoError(t, err)

	decls, _ := out[DeclarationKey].([]Declaration)
	ktest.RequireEqual(t, decls[0].Params, []string{"k", "v"})
}

func TestDirective_DuplicateName(t *testing.T) {
	t.Parallel()
	out := map[string]any{
		DeclarationKey: []Declaration{{Name: "dup"}},
	}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: []string{`@macro dup() { noop }`},
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	ktest.RequireErrorContains(t, err, "duplicate")
}

func TestDirective_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		lines   []string
		wantSub string
	}{
		{"missing brace", []string{`@macro x() const`}, "expected"},
		{"missing parens", []string{`@macro x { noop }`}, "expected"},
		{"empty name", []string{`@macro () { noop }`}, "name is required"},
		{"unclosed", []string{`@macro x() {`, `  noop`}, "unclosed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := runDirective(t, c.lines...)
			ktest.RequireErrorContains(t, err, c.wantSub)
		})
	}
}
