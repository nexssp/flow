package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestFormatCleanError_RendersSnippet(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "flow.nflow")
	os.WriteFile(file, []byte("line one\nline two\nline three\n"), 0o600)

	err := compiler.SourceError(
		compiler.Position{File: file, Line: 2, Col: 6},
		"boom",
	)
	out := formatCleanError(err, false)

	ktest.RequireStringContains(t, out, "line two")
	ktest.RequireStringContains(t, out, "line one")   // context above
	ktest.RequireStringContains(t, out, "line three") // context below
	ktest.RequireStringContains(t, out, "^")
}

func TestFormatCleanError_MultilineArgumentCaret(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "flow.nflow")
	source := "wrap (\n  outer: {\n    key 1\n  }\n)\n"
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := compiler.NewParserWithFileOffset(context.Background(), compiler.NewOperatorTable(), nil, source, file, 0)
	_, err := parser.Parse()
	if err == nil {
		t.Fatal("Parse() succeeded, want malformed nested map error")
	}
	out := formatCleanError(err, false)
	if !strings.Contains(out, file+":3:9") {
		t.Fatalf("rendered diagnostic lacks exact location %s: %s", file+":3:9", out)
	}
	if !strings.Contains(out, "key 1") || !strings.Contains(out, "^") {
		t.Fatalf("rendered diagnostic lacks offending source line or caret: %s", out)
	}
}
