package projection_test

import (
	"io/fs"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	projectionext "github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestProjectionSpreadPositionsFixture(t *testing.T) {
	t.Parallel()

	source, err := fs.ReadFile(projectionext.Bundle(nil).Fixtures, "nflows/spread_positions.nflow")
	ktest.RequireNoError(t, err)
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	input := map[string]any{
		"first":         map[string]any{"spread_winner": "first", "first_only": true, "explicit_winner": "from-first"},
		"second":        map[string]any{"spread_winner": "second", "second_only": true, "explicit_winner": "from-second"},
		"third":         map[string]any{"spread_winner": "third", "third_only": true},
		"nested_first":  map[string]any{"nested_winner": "nested-first", "nested_first_only": true, "nested_after": "first"},
		"nested_second": map[string]any{"nested_winner": "nested-second", "nested_second_only": true, "nested_after": "second"},
	}
	execution, err := runner.Execute(t.Context(), cfg, string(source), "spread_positions.nflow", input)
	ktest.RequireNoError(t, err)
	assertionErrors := runner.EvaluateAssertions(execution.Output, []string{
		`result.spread_winner == "third"`,
		`result.first_only == true`,
		`result.second_only == true`,
		`result.third_only == true`,
		`result.middle == "between-spreads"`,
		`result.after == "before-last-spread"`,
		`result.explicit_winner == "explicit-last"`,
		`result.final == "after-last-spread"`,
		`result.nested.nested_winner == "nested-second"`,
		`result.nested.nested_first_only == true`,
		`result.nested.nested_second_only == true`,
		`result.nested.nested_after == "second"`,
		`result.nested.before_second == "between-nested-spreads"`,
	})
	if len(assertionErrors) != 0 {
		t.Fatalf("assertion errors: %v; output: %#v", assertionErrors, execution.Output)
	}
}
