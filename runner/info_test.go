package runner_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/runner"
)

// TestPrintInfoJSON_NoHTMLEscaping locks in the encoder settings of
// PrintInfoJSON. It is a separate function from cli.writeJSON and
// carries its own SetEscapeHTML(false); if someone removes that line,
// no other test fails.
func TestPrintInfoJSON_NoHTMLEscaping(t *testing.T) {
	var buf bytes.Buffer

	ast := &compiler.Atom{Name: "runtime.noop"}
	err := runner.PrintInfoJSON(&buf, "<embedded>",
		map[string]any{"description": "a<b>c"},
		ast)
	ktest.RequireNoError(t, err)

	out := buf.String()
	ktest.RequireStringContains(t, out, `"<embedded>"`)
	ktest.RequireStringContains(t, out, `"a<b>c"`)
	ktest.RequireStringNotContains(t, out, `\u003c`)
	ktest.RequireStringNotContains(t, out, `\u003e`)
}

func TestRunSourceInfoParseErrorIncludesSourceSnippet(t *testing.T) {
	var stdout, stderr bytes.Buffer
	source := "# context\nruntime.noop ( value: )\n"
	code := runner.RunSource(
		context.Background(),
		runner.Config{Operators: compiler.NewOperatorTable()},
		source,
		"info.nflow",
		nil,
		runner.Opts{Info: true, Stdout: &stdout, Stderr: &stderr},
	)
	ktest.RequireEqual(t, code, 1)
	ktest.RequireEqual(t, stdout.Len(), 0)
	ktest.RequireStringContains(t, stderr.String(), "info.nflow:2")
	ktest.RequireStringContains(t, stderr.String(), "runtime.noop ( value: )")
	ktest.RequireStringContains(t, stderr.String(), "^")
}

func TestRunSourceInfoPreprocessErrorIncludesSourceSnippet(t *testing.T) {
	var stdout, stderr bytes.Buffer
	source := "# context\n@broken\n"
	directive := compiler.Directive{
		Name: "broken",
		Handler: func(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
			return compiler.DirectiveRes{}, compiler.SourceError(
				compiler.Position{File: req.File, Line: req.I + 1}, "preprocess failed",
			)
		},
	}
	code := runner.RunSource(
		context.Background(),
		runner.Config{
			Directives: compiler.NewDirectiveTable(directive),
			Operators:  compiler.NewOperatorTable(),
		},
		source,
		"info.nflow",
		nil,
		runner.Opts{Info: true, Stdout: &stdout, Stderr: &stderr},
	)
	ktest.RequireEqual(t, code, 1)
	ktest.RequireEqual(t, stdout.Len(), 0)
	ktest.RequireStringContains(t, stderr.String(), "info.nflow:2")
	ktest.RequireStringContains(t, stderr.String(), "@broken")
	ktest.RequireStringContains(t, stderr.String(), "^")
}
