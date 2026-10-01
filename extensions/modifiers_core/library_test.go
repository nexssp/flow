package modifiers_core

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresModifiers(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")

	want := []string{"timeout", "retry", "concurrency", "cache", "coalesce", "dedup", "idempotent", "rate_limit"}
	got := map[string]bool{}
	for _, m := range b.Modifiers {
		got[m.Name] = true
	}
	for _, name := range want {
		ktest.RequireCondition(t, got[name], "modifier %q missing", name)
	}
}

func TestDefaultKey_DeterministicAndOrderInsensitive(t *testing.T) {
	t.Parallel()
	a := defaultKey(map[string]any{"b": 2, "a": 1})
	b := defaultKey(map[string]any{"a": 1, "b": 2})
	ktest.RequireEqual(t, a, b)
	ktest.RequireCondition(t, a != "", "empty key for non-nil input")
}

func TestDefaultKey_NilInput(t *testing.T) {
	t.Parallel()
	ktest.RequireEqual(t, defaultKey(nil), "")
}
