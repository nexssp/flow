package runner_test

import (
	"bytes"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/runner"
)

// TestPrintInfoJSON_NoHTMLEscaping locks in the encoder settings of
// PrintInfoJSON. It is a separate function from cli.writeJSON and
// carries its own SetEscapeHTML(false); if someone removes that line,
// no other test fails.
func TestPrintInfoJSON_NoHTMLEscaping(t *testing.T) {
	var buf bytes.Buffer

	ast := &core.Atom{Name: "runtime.noop"}
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
