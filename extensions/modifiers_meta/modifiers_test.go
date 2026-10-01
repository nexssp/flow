package modifiers_meta

import (
	"context"
	"slices"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// TestModifiers_Translation verifies each modifier parses its DSL value
// and configures the matching Meta field. Runtime effect (does the
// catalog display it, does the HTTP transport honor scope) is outside
// this package's responsibility.
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
			name:     "name",
			modifier: "name",
			raw:      "renamed",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Name, "renamed")
			},
		},
		{
			name:     "desc",
			modifier: "desc",
			raw:      "hello",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Description, "hello")
			},
		},
		{
			name:     "description",
			modifier: "description",
			raw:      "hello",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Description, "hello")
			},
		},
		{
			name:     "status",
			modifier: "status",
			raw:      "201",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.SuccessStatus, 201)
			},
		},
		{
			name:     "tag single",
			modifier: "tag",
			raw:      "alpha",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, slices.Contains(meta.Tags, "alpha"), "tag alpha missing")
			},
		},
		{
			name:     "tag list",
			modifier: "tag",
			raw:      "a,b,c",
			verify: func(t *testing.T, meta *action.Meta) {
				for _, want := range []string{"a", "b", "c"} {
					ktest.RequireCondition(t, slices.Contains(meta.Tags, want), "tag %q missing", want)
				}
			},
		},
		{
			name:     "scope public",
			modifier: "scope",
			raw:      "public",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Scope, action.ScopePublic)
			},
		},
		{
			name:     "scope internal",
			modifier: "scope",
			raw:      "internal",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Scope, action.ScopeInternal)
			},
		},
		{
			name:     "scope system",
			modifier: "scope",
			raw:      "system",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Scope, action.ScopeSystem)
			},
		},
		{
			name:     "scope case-insensitive",
			modifier: "scope",
			raw:      "INTERNAL",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Scope, action.ScopeInternal)
			},
		},
		{
			name:     "flag read_only",
			modifier: "read_only",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, slices.Contains(meta.Tags, "read_only"), "read_only tag missing")
			},
		},
		{
			name:     "flag audit",
			modifier: "audit",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, slices.Contains(meta.Tags, "audit"), "audit tag missing")
			},
		},
		{
			name:     "flag debug",
			modifier: "debug",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, slices.Contains(meta.Tags, "debug"), "debug tag missing")
			},
		},
		{
			name:     "flag deprecated",
			modifier: "deprecated",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireCondition(t, slices.Contains(meta.Tags, "deprecated"), "deprecated tag missing")
			},
		},
		{
			name:     "flag strict is a no-op",
			modifier: "strict",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				// strict/lenient are read by core/strict.go via
				// hasModifier; the modifier itself must not mutate Meta.
				ktest.RequireEqual(t, meta.Name, "probe")
			},
		},
		{
			name:     "flag lenient is a no-op",
			modifier: "lenient",
			raw:      "",
			verify: func(t *testing.T, meta *action.Meta) {
				ktest.RequireEqual(t, meta.Name, "probe")
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

func TestModifiers_FlagRejectsValue(t *testing.T) {
	t.Parallel()
	table := core.NewModifierTable(Modifiers()...)

	// Flags take no value; passing one is a DSL authoring mistake the
	// modifier must reject, not silently ignore.
	for _, name := range []string{"read_only", "audit", "debug", "deprecated", "strict", "lenient"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			modifier, ok := table.ByName(name)
			ktest.RequireCondition(t, ok, "modifier %q not registered", name)

			builder := action.New[any, any]("probe", func(_ context.Context, in any) (any, error) {
				return in, nil
			})
			err := modifier.Apply(builder, "unexpected")
			ktest.RequireCondition(t, err != nil, "expected error for :%s=unexpected", name)
		})
	}
}
