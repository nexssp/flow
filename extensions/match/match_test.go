package match_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/match"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/runner"
)

func newTestRunner(t *testing.T) runner.Config {
	t.Helper()
	bundles := []core.Bundle{
		runtime.Bundle(nil),
		match.Bundle(nil),
	}
	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)
	return cfg
}

func TestMatch_SubjectEquality(t *testing.T) {
	t.Parallel()
	cfg := newTestRunner(t)

	dsl := `
match(.code) {
  "URGENT"   -> runtime.const @{ value: "p1" },
  "STANDARD" -> runtime.const @{ value: "p2" },
  _          -> runtime.const @{ value: "p3" }
}
`
	ctx := context.Background()

	// 1. First arm match
	res1, err := runner.Execute(ctx, cfg, dsl, "test1", map[string]any{"code": "URGENT"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res1.Output, "p1")

	// 2. Second arm match
	res2, err := runner.Execute(ctx, cfg, dsl, "test2", map[string]any{"code": "STANDARD"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res2.Output, "p2")

	// 3. Fallback default match
	res3, err := runner.Execute(ctx, cfg, dsl, "test3", map[string]any{"code": "OTHER"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res3.Output, "p3")
}

func TestMatch_BooleanPredicates(t *testing.T) {
	t.Parallel()
	cfg := newTestRunner(t)

	dsl := `
match {
  .danger && .blocked     -> runtime.const @{ value: "critical" },
  .danger && !.blocked    -> runtime.const @{ value: "urgent" },
  !.danger && .blocked    -> runtime.const @{ value: "access_hold" },
  _                       -> runtime.const @{ value: "routine" }
}
`
	ctx := context.Background()

	// Danger and blocked
	r1, err := runner.Execute(ctx, cfg, dsl, "t1", map[string]any{"danger": true, "blocked": true})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, r1.Output, "critical")

	// Danger only
	r2, err := runner.Execute(ctx, cfg, dsl, "t2", map[string]any{"danger": true, "blocked": false})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, r2.Output, "urgent")

	// Neither (fallback)
	r3, err := runner.Execute(ctx, cfg, dsl, "t3", map[string]any{"danger": false, "blocked": false})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, r3.Output, "routine")
}

func TestMatch_NoArmsMatch_PassThrough(t *testing.T) {
	t.Parallel()
	cfg := newTestRunner(t)

	dsl := `
match {
  .score > 90 -> runtime.const @{ value: "A" },
  .score > 80 -> runtime.const @{ value: "B" }
}
`
	ctx := context.Background()
	input := map[string]any{"score": 50, "keep": "unmodified"}

	res, err := runner.Execute(ctx, cfg, dsl, "pass_through", input)
	ktest.RequireNoError(t, err)

	outMap, ok := res.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "expected map[string]any output")
	ktest.RequireEqual(t, outMap["keep"], "unmodified")
	ktest.RequireEqual(t, outMap["score"], 50)
}

func TestMatch_ContextCancellation(t *testing.T) {
	t.Parallel()
	cfg := newTestRunner(t)

	dsl := `
match {
  .slow == true -> runtime.sleep @{ duration_ms: 500 },
  _             -> runtime.const @{ value: "fast" }
}
`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := runner.Execute(ctx, cfg, dsl, "timeout", map[string]any{"slow": true})
	ktest.RequireCondition(t, err != nil, "expected error on context timeout")
	ktest.RequireCondition(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled), "expected context cancellation error")
}

func TestMatch_SyntaxErrors(t *testing.T) {
	t.Parallel()
	cfg := newTestRunner(t)
	ctx := context.Background()

	cases := []struct {
		name string
		dsl  string
	}{
		{
			name: "missing opening brace",
			dsl:  `match(.code) "A" -> runtime.noop }`,
		},
		{
			name: "unclosed parentheses in subject",
			dsl:  `match(.code { "A" -> runtime.noop }`,
		},
		{
			name: "missing arrow",
			dsl:  `match { .danger runtime.const @{ value: 1 } }`,
		},
		{
			name: "unclosed closing brace",
			dsl:  `match { .danger -> runtime.const @{ value: 1 }`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := runner.Execute(ctx, cfg, tc.dsl, tc.name, map[string]any{})
			ktest.RequireCondition(t, err != nil, "expected parse/compile error for invalid syntax: %s", tc.dsl)
		})
	}
}

func TestMatch_ConcurrentExecutionRace(t *testing.T) {
	t.Parallel()
	cfg := newTestRunner(t)

	dsl := `
match(.role) {
  "admin"  -> runtime.const @{ value: { auth: true, tier: "admin" } },
  "user"   -> runtime.const @{ value: { auth: true, tier: "user" } },
  _        -> runtime.const @{ value: { auth: false, tier: "guest" } }
}
`
	const workers = 32
	xtest.RunParallel(t, workers, func(idx int) error {
		role := "guest"
		switch idx % 3 {
		case 0:
			role = "admin"
		case 1:
			role = "user"
		}

		res, err := runner.Execute(context.Background(), cfg, dsl, "parallel_test", map[string]any{"role": role})
		if err != nil {
			return err
		}

		out, ok := res.Output.(map[string]any)
		if !ok {
			return errors.New("expected map output")
		}
		if out["tier"] != role {
			return errors.New("tier mismatch under concurrent execution")
		}
		return nil
	})
}
