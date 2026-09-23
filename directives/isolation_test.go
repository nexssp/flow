package directives_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFlowDoesNotImportAI verifies that the flow module has no
// transitive dependency on any ai/* package.
//
// The test locates the flow module root via runtime.Caller, then runs
// `go list -deps` there. It is skipped under -short because go list is
// slow.
func TestFlowDoesNotImportAI(t *testing.T) {

	if testing.Short() {
		t.Skip("go list is slow; skipped under -short")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	// thisFile is .../flow/directives/isolation_test.go
	moduleRoot := filepath.Dir(filepath.Dir(thisFile))

	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "./...")
	cmd.Dir = moduleRoot

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "github.com/nexssp/ai/") {
			t.Errorf("flow module transitively imports ai package: %s", line)
		}
	}
}
