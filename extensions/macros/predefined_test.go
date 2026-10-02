package macros_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestBuiltinMacros(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		macros.Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	// Declare at least one macro to activate macro processor in OnPreprocess
	dsl := `
@macro init_check() {
  runtime.noop
}

@init_check()
-> @check_required(user_id)
-> @check_choice(status, ["active", "pending"])
-> @check_range(score, 0, 100)
`

	ctx := context.Background()

	// 1. Valid input passes
	valid := map[string]any{
		"user_id": "usr_99",
		"status":  "active",
		"score":   85,
	}
	_, err = runner.Execute(ctx, cfg, dsl, "valid", valid)
	ktest.RequireNoError(t, err)

	// 2. Invalid choice fails
	invalidChoice := map[string]any{
		"user_id": "usr_99",
		"status":  "suspended",
		"score":   85,
	}
	_, err = runner.Execute(ctx, cfg, dsl, "invalid_choice", invalidChoice)
	ktest.RequireCondition(t, err != nil, "expected error on invalid choice")

	// 3. Out of range score fails
	invalidRange := map[string]any{
		"user_id": "usr_99",
		"status":  "active",
		"score":   150,
	}
	_, err = runner.Execute(ctx, cfg, dsl, "invalid_range", invalidRange)
	ktest.RequireCondition(t, err != nil, "expected error on out of range score")
}
