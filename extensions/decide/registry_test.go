package decide

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

type stubBackend struct {
	name   string
	result Result
	err    error
}

func (s stubBackend) Name() string { return s.name }
func (s stubBackend) Close() error { return nil }
func (s stubBackend) Decide(context.Context, map[string]any, map[string]any) (Result, error) {
	return s.result, s.err
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	backend := stubBackend{name: "alpha", result: Result{Answers: map[string]Answer{}}}
	ktest.RequireNoError(t, r.Register(backend))

	got, ok := r.Lookup("alpha")
	ktest.RequireCondition(t, ok, "backend not found")
	ktest.RequireEqual(t, got.Name(), "alpha")
}

func TestRegistry_RejectsNilAndEmpty(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	ktest.RequireCondition(t, r.Register(nil) != nil, "nil backend should fail")
	ktest.RequireCondition(t, r.Register(stubBackend{}) != nil, "empty name should fail")
}

func TestRegistry_Duplicate(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	backend := stubBackend{name: "dup"}
	ktest.RequireNoError(t, r.Register(backend))
	ktest.RequireCondition(t, r.Register(backend) != nil, "duplicate should fail")
}

func TestRegistry_Names(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	for _, name := range []string{"c", "a", "b"} {
		ktest.RequireNoError(t, r.Register(stubBackend{name: name}))
	}
	ktest.RequireEqual(t, r.Names(), []string{"a", "b", "c"})
}
