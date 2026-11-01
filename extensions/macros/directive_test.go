package macros

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

// runDirective drives handleDirective with a single line, returning
// the mutated Out map.
func runDirective(tb testing.TB, lines ...string) (map[string]any, compiler.DirectiveRes, error) {
	tb.Helper()
	out := map[string]any{}
	res, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	return out, res, err
}

func TestDirective_InlineBody(t *testing.T) {
	t.Parallel()
	out, res, err := runDirective(t, `@macro hi() { runtime.const @{ value: "hi" } }`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res.Next, 1)

	decls, _ := out[DeclarationKey].([]Declaration)
	ktest.RequireEqual(t, len(decls), 1)
	ktest.RequireEqual(t, decls[0].Name, "hi")
	ktest.RequireEqual(t, decls[0].Body, `runtime.const @{ value: "hi" }`)
	ktest.RequireEqual(t, len(decls[0].Params), 0)
	ktest.RequireEqual(t, decls[0].DefFile, "<test>")
}

func TestDirective_MultiLineBody(t *testing.T) {
	t.Parallel()
	lines := []string{
		`@macro echo(v) {`,
		`  runtime.const @{ value: $v }`,
		`}`,
	}
	out, res, err := runDirective(t, lines...)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res.Next, 3)

	decls, _ := out[DeclarationKey].([]Declaration)
	ktest.RequireEqual(t, len(decls), 1)
	ktest.RequireEqual(t, decls[0].Name, "echo")
	ktest.RequireEqual(t, decls[0].Params, []string{"v"})
	ktest.RequireEqual(t, decls[0].Body, `runtime.const @{ value: $v }`)
}

func TestDirective_ExactTypedParamKindSet(t *testing.T) {
	t.Parallel()
	out, _, err := runDirective(t, `@macro pair(key: Identifier, label: String, count: Integer, value: Expr, action: ActionRef) { $key: $value }`)
	ktest.RequireNoError(t, err)

	decls, _ := out[DeclarationKey].([]Declaration)
	ktest.RequireEqual(t, decls[0].Params, []string{"key", "label", "count", "value", "action"})
	ktest.RequireEqual(t, decls[0].ParamKinds, []string{"Identifier", "String", "Integer", "Expr", "ActionRef"})
}

func TestDirective_HyphenatedMacroName(t *testing.T) {
	t.Parallel()
	out, _, err := runDirective(t, `@macro build-report() { runtime.noop }`)
	ktest.RequireNoError(t, err)
	decls, _ := out[DeclarationKey].([]Declaration)
	ktest.RequireEqual(t, decls[0].Name, "build-report")
}

func TestDirective_DuplicateNameUsesCompilerRegistry(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	req := compiler.DirectiveReq{
		Lines: []string{`@macro dup() { noop }`},
		I:     0,
		Out:   out,
		File:  "<test>",
	}
	_, err := handleDirective(context.Background(), req)
	ktest.RequireNoError(t, err)
	_, err = handleDirective(context.Background(), req)
	ktest.RequireErrorContains(t, err, `duplicate declaration @macro "dup"`)
	ktest.RequireErrorContains(t, err, "first declared at")
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
		{"qualified name", []string{`@macro report.echo() { noop }`}, "invalid name"},
		{"invalid parameter", []string{`@macro f(bad-name) { noop }`}, "invalid parameter name"},
		{"duplicate parameter", []string{`@macro f(value, value) { noop }`}, "duplicate parameter"},
		{"unsupported parameter kind", []string{`@macro f(value:number) { noop }`}, "unsupported kind"},
		{"lowercase kind is unsupported", []string{`@macro f(value:expr) { noop }`}, "unsupported kind"},
		{"PipelineRef is deferred", []string{`@macro f(value:PipelineRef) { noop }`}, "unsupported kind"},
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
