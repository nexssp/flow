package projection_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"

	projectionext "github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestProjectionBindingConflictRuntimeError(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	const conflictFixture = "nflows/ambiguous_binding_runtime.nflow"
	conflictSource, err := fs.ReadFile(projectionext.Bundle(nil).Fixtures, conflictFixture)
	if err != nil {
		t.Fatalf("read fixture %q: %v", conflictFixture, err)
	}
	input := map[string]any{"tool": map[string]any{"name": "payload"}}
	_, err = runner.Execute(t.Context(), cfg, string(conflictSource), conflictFixture, input)
	if xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("execute ambiguous fixture error = %v, want Conflict", err)
	}
	for _, want := range []string{
		`ambiguous bare identifier "tool"`,
		"runtime input has a field",
		"was not evaluated",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("runtime diagnostic %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "payload") {
		t.Errorf("runtime diagnostic leaked fixture value: %v", err)
	}
}
