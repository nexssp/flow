package modifiers_core

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// TestModifiers_Translation verifies each modifier parses its DSL value
// and configures the matching Meta field. Runtime behavior of the
// installed middleware is the kernel's responsibility — see
// kernel/action/resilience_test.go, dedup_coalesce_test.go, and
// rate_limit_test.go.
func TestModifiers_Translation(t *testing.T) {
	t.Parallel()
	table := core.NewModifierTable(Modifiers()...)

	cases := []struct {
		name     string
		modifier string
		raw      string
		verify   func(t *testing.T, meta *action.Meta)
	}{
		{
			name:     "timeout",
			modifier: "timeout",
			raw:      "2s",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Timeout, 2*time.Second)
			},
		},
		{
			name:     "retry",
			modifier: "retry",
			raw:      "5",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.RetryMax, 5)
			},
		},
		{
			name:     "concurrency",
			modifier: "concurrency",
			raw:      "16",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.ConcurrencyLimit, int32(16))
			},
		},
		{
			name:     "cache",
			modifier: "cache",
			raw:      "30s",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.CacheTTL, 30*time.Second)
			},
		},
		{
			name:     "coalesce",
			modifier: "coalesce",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, meta.Coalesced, "Coalesced flag not set")
			},
		},
		{
			name:     "dedup",
			modifier: "dedup",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, meta.Deduplicated, "Deduplicated flag not set")
			},
		},
		{
			name:     "idempotent",
			modifier: "idempotent",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, meta.Idempotency.Enabled, "Idempotency.Enabled not set")
			},
		},
		{
			name:     "rate_limit",
			modifier: "rate_limit",
			raw:      "100",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, meta.RateLimit != "", "RateLimit not set")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			modifier, ok := table.ByName(tc.modifier)
			ktest.RequireCondition(t, ok, "modifier %q not registered", tc.modifier)

			builder := action.New[any, any]("probe", func(_ context.Context, in any) (any, error) {
				return in, nil
			})
			ktest.RequireNoError(t, modifier.Apply(builder, tc.raw))
			tc.verify(t, builder.Describe())
		})
	}
}

func TestModifiers_RejectInvalidValue(t *testing.T) {
	t.Parallel()
	table := core.NewModifierTable(Modifiers()...)

	cases := []struct {
		name     string
		modifier string
		raw      string
	}{
		{"timeout non-duration", "timeout", "abc"},
		{"retry non-int", "retry", "xyz"},
		{"concurrency overflow", "concurrency", "99999999999999"},
		{"cache non-duration", "cache", "forever"},
		{"rate_limit non-int", "rate_limit", "many"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			modifier, ok := table.ByName(tc.modifier)
			ktest.RequireCondition(t, ok, "modifier %q not registered", tc.modifier)

			builder := action.New[any, any]("probe", func(_ context.Context, in any) (any, error) {
				return in, nil
			})
			err := modifier.Apply(builder, tc.raw)
			ktest.RequireCondition(t, err != nil, "expected parse error for :%s=%q", tc.modifier, tc.raw)
		})
	}
}
