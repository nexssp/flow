package projection_test

import (
	"io/fs"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	projectionext "github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestProjectionPathSyntaxFixtures(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	for _, tc := range []struct {
		fixture string
		asserts []string
	}{
		{
			fixture: "nflows/path_shorthand_spread.nflow",
			asserts: []string{
				`result.attempt_id == "attempt-7"`,
				`result.keep == true`,
				`result.override == "new"`,
			},
		},
		{
			fixture: "nflows/path_argument_shorthand.nflow",
			asserts: []string{`result.id == "arg-17"`},
		},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			source, readErr := fs.ReadFile(projectionext.Bundle(nil).Fixtures, tc.fixture)
			ktest.RequireNoError(t, readErr)
			execution, execErr := runner.Execute(t.Context(), cfg, string(source), tc.fixture, nil)
			ktest.RequireNoError(t, execErr)
			ktest.RequireEqual(t, len(runner.EvaluateAssertions(execution.Output, tc.asserts)), 0)
		})
	}
}
