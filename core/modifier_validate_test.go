package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestValidateModifierValue(t *testing.T) {
	tests := []struct {
		name    string
		kind    core.ModifierKind
		value   string
		wantErr bool
	}{
		{"any accepts anything", core.ModifierKindAny, "anything", false},
		{"flag empty ok", core.ModifierKindFlag, "", false},
		{"flag with value fails", core.ModifierKindFlag, "yes", true},
		{"string accepts anything", core.ModifierKindString, "any", false},
		{"string list ok", core.ModifierKindStringList, "a,b", false},
		{"string list empty fails", core.ModifierKindStringList, "", true},
		{"int ok", core.ModifierKindInt, "42", false},
		{"int fails on alpha", core.ModifierKindInt, "abc", true},
		{"int32 ok", core.ModifierKindInt32, "42", false},
		{"int32 overflow fails", core.ModifierKindInt32, "9999999999", true},
		{"int64 ok", core.ModifierKindInt64, "9223372036854775807", false},
		{"float64 ok", core.ModifierKindFloat64, "3.14", false},
		{"float64 fails on alpha", core.ModifierKindFloat64, "abc", true},
		{"duration ok", core.ModifierKindDuration, "5s", false},
		{"duration fails on alpha", core.ModifierKindDuration, "abc", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := core.ValidateModifierValue(tc.kind, tc.value)
			if tc.wantErr {
				ktest.RequireCondition(t, err != nil, "expected error")
			} else {
				ktest.RequireNoError(t, err)
			}
		})
	}
}

func TestModifierTable_RejectsDuplicateNonRepeatable(t *testing.T) {
	table := core.NewModifierTable(
		core.WithUnique(core.Int("count", func(b *action.Builder[any, any], _ int) *action.Builder[any, any] {
			return b
		})),
	)
	act := action.New("test", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	_, err := table.ApplyAll(act, []string{"count=1", "count=2"})
	ktest.RequireCondition(t, err != nil, "expected duplicate error")
	ktest.RequireStringContains(t, err.Error(), "not repeatable")
}

func TestModifierTable_RejectsInvalidValueKind(t *testing.T) {
	table := core.NewModifierTable(
		core.Duration("wait", func(b *action.Builder[any, any], _ time.Duration) *action.Builder[any, any] {
			return b
		}),
	)
	act := action.New("test", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	_, err := table.ApplyAll(act, []string{"wait=abc"})
	ktest.RequireCondition(t, err != nil, "expected value-kind error")
	ktest.RequireStringContains(t, err.Error(), "expected duration")
}

func TestModifierTable_RejectsDuplicateUnique(t *testing.T) {
	table := core.NewModifierTable(
		core.WithUnique(core.Int("count", func(b *action.Builder[any, any], _ int) *action.Builder[any, any] {
			return b
		})),
	)
	act := action.New("test", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	_, err := table.ApplyAll(act, []string{"count=1", "count=2"})
	ktest.RequireCondition(t, err != nil, "expected duplicate error")
	ktest.RequireStringContains(t, err.Error(), "not repeatable")
}
