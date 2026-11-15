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

func TestProjectionBindingConflictNFlowFixtures(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	const validFixture = "nflows/explicit_binding_disambiguation.nflow"
	validSource, err := fs.ReadFile(projectionext.Bundle(nil).Fixtures, validFixture)
	if err != nil {
		t.Fatalf("read fixture %q: %v", validFixture, err)
	}
	validResult, err := runner.Execute(t.Context(), cfg, string(validSource), validFixture, nil)
	if err != nil {
		t.Fatalf("execute explicit disambiguation fixture: %v", err)
	}
	assertions := []string{
		`result.name == "current"`,
		`result.original == "original"`,
		`result.tool.name == "current"`,
	}
	if failures := runner.EvaluateAssertions(validResult.Output, assertions); len(failures) != 0 {
		t.Fatalf("explicit disambiguation assertions failed: %v", failures)
	}

	const conflictFixture = "nflows/ambiguous_binding_runtime.nflow"
	conflictSource, err := fs.ReadFile(projectionext.Bundle(nil).Fixtures, conflictFixture)
	if err != nil {
		t.Fatalf("read fixture %q: %v", conflictFixture, err)
	}
	_, err = runner.Execute(t.Context(), cfg, string(conflictSource), conflictFixture, nil)
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
