package core_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest"

	"github.com/nexssp/flow/core"
)

// ApplyAll must not allocate per modifier; a 100-modifier atom costs the
// same as a 1-modifier atom. The absolute count is irrelevant — the
// contract is that it stays flat as modifier count grows.
//
//nolint:paralleltest // testing.AllocsPerRun cannot be called from a parallel test
func TestApplyAll_ConstantAllocs(t *testing.T) {
	target := action.New("target", func(_ context.Context, in any) (any, error) { return in, nil }).Build()

	noop := func(_ *action.Builder[any, any], _ string) error { return nil }
	table := core.NewModifierTable(core.Modifier{Name: "a", Apply: noop})

	oneAllocs := xtest.AllocsPerRun(200, func() {
		_, _ = table.ApplyAll(target, []string{"a=1"})
	})

	hundred := make([]string, 100)
	for i := range hundred {
		hundred[i] = "a=1"
	}
	hundredAllocs := xtest.AllocsPerRun(200, func() {
		_, _ = table.ApplyAll(target, hundred)
	})

	// AllocsPerRun returns a rounded average; a ±1 wobble between runs
	// is expected. The contract is "does not scale with modifier
	// count", not bit-identical counts.
	if hundredAllocs > oneAllocs+1 {
		t.Fatalf("allocs scale with modifier count: one=%.0f hundred=%.0f", oneAllocs, hundredAllocs)
	}
}

func TestApplyAll_AppliesModifiersInOrder(t *testing.T) {
	t.Parallel()
	target := action.New("target", func(_ context.Context, in any) (any, error) { return in, nil }).Build()

	var applied []string
	capture := func(_ *action.Builder[any, any], raw string) error {
		applied = append(applied, raw)
		return nil
	}

	table := core.NewModifierTable(
		core.Modifier{Name: "a", Apply: capture},
		core.Modifier{Name: "b", Apply: capture},
	)

	if _, err := table.ApplyAll(target, []string{"a=1", "b", "a=2"}); err != nil {
		t.Fatal(err)
	}

	want := []string{"1", "", "2"}
	if !slices.Equal(applied, want) {
		t.Fatalf("applied = %v, want %v", applied, want)
	}
}

func TestApplyAll_RejectsUnknownModifier(t *testing.T) {
	t.Parallel()
	target := action.New("target", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	table := core.NewModifierTable(core.Modifier{
		Name:  "a",
		Apply: func(_ *action.Builder[any, any], _ string) error { return nil },
	})

	_, err := table.ApplyAll(target, []string{"a=1", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown modifier")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("error should mention 'bogus', got: %v", err)
	}
}

func TestApplyAll_PassesValueToModifier(t *testing.T) {
	t.Parallel()
	target := action.New("target", func(_ context.Context, in any) (any, error) { return in, nil }).Build()

	var captured string
	table := core.NewModifierTable(core.Modifier{
		Name: "x",
		Apply: func(_ *action.Builder[any, any], raw string) error {
			captured = raw
			return nil
		},
	})

	if _, err := table.ApplyAll(target, []string{"x=hello world"}); err != nil {
		t.Fatal(err)
	}
	if captured != "hello world" {
		t.Fatalf("captured = %q, want %q", captured, "hello world")
	}
}

func TestNewModifierTable_RejectsDuplicate(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate modifier")
		}
	}()
	core.NewModifierTable(
		core.Modifier{Name: "a", Apply: func(*action.Builder[any, any], string) error { return nil }},
		core.Modifier{Name: "a", Apply: func(*action.Builder[any, any], string) error { return nil }},
	)
}
