package runner_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

// withBundles is native plus the projection library. Projection's
// primary extension is registered by runner.BuildConfig, so only the
// library is supplied here.
func withBundles() []core.Bundle {
	return native.Bundles()
}

func TestWith_MergesArgsIntoInput(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(withBundles())
	ktest.RequireNoError(t, err)

	execution, err := runner.Execute(
		context.Background(),
		cfg,
		`{ goal: "x", attempt: 1 } -> with @{ note: "bump", approved: true }`,
		"with_test.nflow",
		nil,
	)
	ktest.RequireNoError(t, err)

	output, ok := execution.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "expected map, got %T", execution.Output)

	ktest.RequireEqual(t, output["goal"], "x")
	ktest.RequireEqual(t, output["note"], "bump")
	ktest.RequireEqual(t, output["approved"], true)

	// attempt is produced by projection, which runs through expr-lang.
	// expr returns Go int for integer literals, not float64 — asserting
	// the exact type catches a silent switch to JSON round-tripping.
	attempt, ok := output["attempt"].(int)
	ktest.RequireCondition(t, ok,
		"attempt must be int (expr result), got %T: %v",
		output["attempt"], output["attempt"])
	ktest.RequireEqual(t, attempt, 1)
}
