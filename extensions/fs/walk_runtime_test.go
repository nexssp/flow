package fs

import (
	"bytes"
	stdfs "io/fs"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestWalkFailOnErrorNFlowFixture(t *testing.T) {
	source, err := stdfs.ReadFile(Bundle(nil).Fixtures, "nflows/walk.nflow")
	ktest.RequireNoError(t, err)

	workingDir := t.TempDir()
	writeFile(t, filepath.Join(workingDir, "walk--a.txt"), "a")
	writeFile(t, filepath.Join(workingDir, "walk--sub", "walk--b.txt"), "b")
	t.Chdir(workingDir)

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		syntax.Bundle(nil),
		description.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, string(source), "walk.nflow", map[string]any{}, runner.Opts{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("RunSource returned %d; stdout=%q; stderr=%q", code, stdout.String(), stderr.String())
	}
	ktest.RequireEqual(t, stderr.String(), "")
}
