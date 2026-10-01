package modifiers_auth

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// TestModifiers_Translation verifies each identity guard sets the
// matching Meta fields. The actual authentication and authorization
// checks live in kernel/action/builder_authz.go and are covered by
// the kernel's own test suite.
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
			name:     "auth",
			modifier: "auth",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, meta.RequiresAuth, "RequiresAuth not set")
			},
		},
		{
			name:     "role single",
			modifier: "role",
			raw:      "admin",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, meta.RequiresAuth, "RequiresAuth not set")
				ktest.RequireEqual(t, meta.RequiredRoles, []string{"admin"})
			},
		},
		{
			name:     "role list",
			modifier: "role",
			raw:      "admin,editor",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.RequiredRoles, []string{"admin", "editor"})
			},
		},
		{
			name:     "perm",
			modifier: "perm",
			raw:      "payments:write",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, meta.RequiresAuth, "RequiresAuth not set")
				ktest.RequireEqual(t, meta.RequiredPermissions, []string{"payments:write"})
			},
		},
		{
			name:     "feature",
			modifier: "feature",
			raw:      "beta,alpha",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.RequiredFeatures, []string{"beta", "alpha"})
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
