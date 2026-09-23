package flow_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/nodes/fsio"
	"github.com/nexssp/kernel/action"
)

// TestEndToEnd_FsWalkFilterStdout_RealFiles proves the whole stack wires
// up: fsio.Library → flow.Registry → CompilePipeline → DoAny.
//
// It creates real temporary files, streams them, filters out test files
// and non-go files, sorts them, and captures the stdout output for
// exact comparison.
func TestEndToEnd_FsWalkFilterStdout_RealFiles(t *testing.T) {
	// Cannot be parallel: os.Stdout is hijacked for the duration.

	dir := t.TempDir()
	files := []string{
		"main.go",
		"utils.go",
		"readme.md",
		".hidden.go",
		"main_test.go",
	}
	for _, f := range files {
		_ = os.WriteFile(filepath.Join(dir, f), []byte("package test"), 0o600)
	}

	reg := flow.NewRegistry()
	if err := reg.Register(fsio.Library()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	prev := flow.DefaultRegistry()
	flow.SetDefaultRegistry(reg)
	t.Cleanup(func() { flow.SetDefaultRegistry(prev) })

	kernelReg := action.MustNewRegistry()

	// tests=true tells fs.filter to DROP test files. The value on the
	// modifier is a boolean: true means "filter out tests".
	dsl := `fs.walk -> stream.filter:ext="go":tests=true -> fs.sort:by="rel_path" -> out.stdout`

	bld, err := flow.CompilePipeline(
		dsl,
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err != nil {
		t.Fatalf("CompilePipeline: %v", err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	defer func() {
		os.Stdout = oldStdout
	}()

	req := map[string]any{"dir": dir}
	_, err = bld.Build().DoAny(context.Background(), req)

	w.Close()
	var outBytes bytes.Buffer
	_, _ = io.Copy(&outBytes, r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("DoAny execution failed: %v", err)
	}

	output := strings.TrimSpace(outBytes.String())
	lines := strings.Split(output, "\n")

	// Expect exactly main.go and utils.go:
	//   - readme.md      rejected by ext="go"
	//   - .hidden.go     skipped by fs.walk (hidden file default)
	//   - main_test.go   dropped by tests=true
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines of output, got %d:\n%s", len(lines), output)
	}
	if lines[0] != "main.go" || lines[1] != "utils.go" {
		t.Errorf("unexpected output lines (sorting may have failed):\n%s", output)
	}
}
