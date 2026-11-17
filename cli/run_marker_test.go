package cli_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmoke_HarnessMarkerDoesNotSuppressHarnessBuild re-asserts the dispatch
// contract of patch 0007 on the current tree: a plain `nflow` engine invoked
// with the NFLOW_HARNESS marker set (as happens when a harness child execs
// the engine for its own sub-flow) must still prepare a harness for external
// requirements it does not carry — the marker alone must not force the
// in-process path that fails with "bundle ... is not registered".
//
// Skipped in -short mode.
func TestSmoke_HarnessMarkerDoesNotSuppressHarnessBuild(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("smoke test; skipped in short mode")
	}
	isolateHarnessCache(t)
	goBin := requireTool(t, "go")
	flowRoot := flowModuleRoot(t)
	kernelRoot := kernelModuleRoot(t, flowRoot)

	workDir := t.TempDir()
	writeForkModule(t, workDir, flowRoot, kernelRoot)
	writeFile(t, filepath.Join(workDir, "test.nflow"), `@description "fork smoke under an inherited harness marker"
@require ./mylib
@assert: result == "hello from fork"
mylib.hello
`)

	cmd := exec.CommandContext(t.Context(), goBin, "run", "./cmd/nflow", "run", filepath.Join(workDir, "test.nflow"))
	cmd.Dir = flowRoot
	cmd.Env = append(cmd.Environ(), "NFLOW_HARNESS=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("nflow run with NFLOW_HARNESS=1 failed: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
			err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "hello from fork") {
		t.Fatalf("expected greeting in output; got:\n%s", stdout.String())
	}
}
