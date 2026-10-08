package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestFormatCleanError_RendersSnippet(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "flow.nflow")
	os.WriteFile(file, []byte("line one\nline two\nline three\n"), 0o600)

	err := core.SourceError(
		core.Position{File: file, Line: 2, Col: 6},
		"boom",
	)
	out := formatCleanError(err, false)

	ktest.RequireStringContains(t, out, "line two")
	ktest.RequireStringContains(t, out, "line one")   // context above
	ktest.RequireStringContains(t, out, "line three") // context below
	ktest.RequireStringContains(t, out, "^")
}
