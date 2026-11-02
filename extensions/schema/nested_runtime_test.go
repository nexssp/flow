package schema_test

import (
	"io/fs"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	schemaext "github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestNestedSchemaFixtures(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	for _, tc := range []struct {
		fixture string
		asserts []string
	}{
		{
			fixture: "nflows/nested_required_rejects.nflow",
			asserts: []string{`result.rejected == true`},
		},
		{
			fixture: "nflows/nested_list_element_rejects.nflow",
			asserts: []string{`result.rejected == true`},
		},
		{
			fixture: "nflows/nested_default.nflow",
			asserts: []string{`result.attempt.expected_currency == "USD"`},
		},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			source, readErr := fs.ReadFile(schemaext.Bundle(nil).Fixtures, tc.fixture)
			ktest.RequireNoError(t, readErr)
			execution, execErr := runner.Execute(t.Context(), cfg, string(source), tc.fixture, nil)
			ktest.RequireNoError(t, execErr)
			ktest.RequireEqual(t, len(runner.EvaluateAssertions(execution.Output, tc.asserts)), 0)
		})
	}
}
