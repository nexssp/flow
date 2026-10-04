package cli

import (
	"bytes"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestWriteJSON_NoHTMLEscaping(t *testing.T) {
	var buf bytes.Buffer
	ktest.RequireNoError(t, writeJSON(&buf, map[string]string{
		"path": "<embedded>",
		"a&b":  "x>y",
	}, true))

	out := buf.String()
	ktest.RequireStringContains(t, out, `"<embedded>"`)
	ktest.RequireStringContains(t, out, `"x>y"`)
	ktest.RequireStringContains(t, out, `"a&b"`)
	ktest.RequireStringNotContains(t, out, `\u003c`)
	ktest.RequireStringNotContains(t, out, `\u003e`)
	ktest.RequireStringNotContains(t, out, `\u0026`)
}

func TestWriteJSON_Pretty(t *testing.T) {
	var buf bytes.Buffer
	ktest.RequireNoError(t, writeJSON(&buf, map[string]int{"a": 1}, true))

	// Two-space indent, newline-terminated.
	ktest.RequireStringContains(t, buf.String(), "  \"a\": 1")
	ktest.RequireStringContains(t, buf.String(), "\n")
}

func TestWriteJSON_Compact(t *testing.T) {
	var buf bytes.Buffer
	ktest.RequireNoError(t, writeJSON(&buf, map[string]int{"a": 1}, false))

	// No indentation, single line, newline-terminated.
	ktest.RequireEqual(t, buf.String(), "{\"a\":1}\n")
}
