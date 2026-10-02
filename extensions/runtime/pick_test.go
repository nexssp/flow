package runtime_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/runner"
)

func TestPick_AllowlistAndDenylist(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig([]core.Bundle{runtime.Bundle(nil)})
	ktest.RequireNoError(t, err)

	ctx := context.Background()

	// 1. Allowlist mode
	dslAllow := `runtime.pick @{ only: ["id", "status"] }`
	input := map[string]any{
		"id":        101,
		"status":    "active",
		"internal":  "secret",
		"temporary": "dump",
	}

	res, err := runner.Execute(ctx, cfg, dslAllow, "allow", input)
	ktest.RequireNoError(t, err)
	outMap, ok := res.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "expected map output")
	ktest.RequireEqual(t, len(outMap), 2)
	ktest.RequireEqual(t, outMap["id"], 101)
	ktest.RequireEqual(t, outMap["status"], "active")

	// 2. Denylist mode
	dslDrop := `runtime.pick @{ drop: ["internal", "temporary"] }`
	resDrop, err := runner.Execute(ctx, cfg, dslDrop, "drop", input)
	ktest.RequireNoError(t, err)
	outDrop, ok := resDrop.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "expected map output")
	ktest.RequireEqual(t, len(outDrop), 2)
	ktest.RequireEqual(t, outDrop["id"], 101)
	ktest.RequireEqual(t, outDrop["status"], "active")
}
