package fsio_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow/nodes/fsio"
	"github.com/nexssp/flow/runner/testkit"
)

func TestOutFile_Pipeline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcFile := filepath.Join(dir, "input.txt")
	outFile := filepath.Join(dir, "out.txt")

	content := "streaming data test payload for out.file"
	if err := os.WriteFile(srcFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Build the fully composed stream DSL instruction exactly as requested
	dsl := `fs.walk:dir="` + filepath.ToSlash(dir) + `":ext="txt" -> fs.read -> out.file:path="` + filepath.ToSlash(outFile) + `"`

	// Execute via Testkit Engine (which compiles streaming pipelines internally)
	res := testkit.RunDSL(t, dsl, nil, fsio.Library())
	res.ExpectSuccess(t)

	written, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if string(written) != content {
		t.Errorf("got %q, want %q", string(written), content)
	}
}
