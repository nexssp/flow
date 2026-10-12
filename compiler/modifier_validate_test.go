package compiler_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestValidateModifierValue(t *testing.T) {
	tests := []struct {
		name    string
		kind    compiler.ModifierKind
		value   string
		wantErr bool
	}{
		{"any accepts anything", compiler.ModifierKindAny, "anything", false},
		{"flag empty ok", compiler.ModifierKindFlag, "", false},
		{"flag with value fails", compiler.ModifierKindFlag, "yes", true},
		{"string accepts anything", compiler.ModifierKindString, "any", false},
		{"string list ok", compiler.ModifierKindStringList, "a,b", false},
		{"string list empty fails", compiler.ModifierKindStringList, "", true},
		{"int ok", compiler.ModifierKindInt, "42", false},
		{"int fails on alpha", compiler.ModifierKindInt, "abc", true},
		{"int32 ok", compiler.ModifierKindInt32, "42", false},
		{"int32 overflow fails", compiler.ModifierKindInt32, "9999999999", true},
		{"int64 ok", compiler.ModifierKindInt64, "9223372036854775807", false},
		{"float64 ok", compiler.ModifierKindFloat64, "3.14", false},
		{"float64 fails on alpha", compiler.ModifierKindFloat64, "abc", true},
		{"duration ok", compiler.ModifierKindDuration, "5s", false},
		{"duration fails on alpha", compiler.ModifierKindDuration, "abc", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := compiler.ValidateModifierValue(tc.kind, tc.value)
			if tc.wantErr {
				ktest.RequireCondition(t, err != nil, "expected error")
			} else {
				ktest.RequireNoError(t, err)
			}
		})
	}
}

func TestModifierTable_RejectsDuplicateNonRepeatable(t *testing.T) {
	table := compiler.NewModifierTable(
		compiler.WithUnique(compiler.Int("count", func(b *action.Builder[any, any], _ int) *action.Builder[any, any] {
			return b
		})),
	)
	act := action.New("test", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	_, err := table.ApplyAll(act, []string{"count=1", "count=2"})
	ktest.RequireCondition(t, err != nil, "expected duplicate error")
	ktest.RequireStringContains(t, err.Error(), "not repeatable")
}

func TestModifierTable_RejectsInvalidValueKind(t *testing.T) {
	table := compiler.NewModifierTable(
		compiler.Duration("wait", func(b *action.Builder[any, any], _ time.Duration) *action.Builder[any, any] {
			return b
		}),
	)
	act := action.New("test", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	_, err := table.ApplyAll(act, []string{"wait=abc"})
	ktest.RequireCondition(t, err != nil, "expected value-kind error")
	ktest.RequireStringContains(t, err.Error(), "expected duration")
}

func TestModifierTable_RejectsDuplicateUnique(t *testing.T) {
	table := compiler.NewModifierTable(
		compiler.WithUnique(compiler.Int("count", func(b *action.Builder[any, any], _ int) *action.Builder[any, any] {
			return b
		})),
	)
	act := action.New("test", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	_, err := table.ApplyAll(act, []string{"count=1", "count=2"})
	ktest.RequireCondition(t, err != nil, "expected duplicate error")
	ktest.RequireStringContains(t, err.Error(), "not repeatable")
}
