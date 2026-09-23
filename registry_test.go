package flow_test

import (
	"context"
	"iter"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

type fakeSource[T any] struct {
	name  string
	items []T
}

func (f *fakeSource[T]) Describe() *action.Meta                                  { return &action.Meta{Name: f.name} }
func (f *fakeSource[T]) GetBindings() []action.Binding                           { return nil }
func (f *fakeSource[T]) ReqPayload() any                                         { return struct{}{} }
func (f *fakeSource[T]) ResPayload() any                                         { var z T; return z }
func (f *fakeSource[T]) GetAnyHooks() []action.AnyHook                           { return nil }
func (f *fakeSource[T]) AddAnyHook(...action.AnyHook)                            {}
func (f *fakeSource[T]) CloneWithHooks(...action.AnyHook) action.AnyStreamAction { return f }

func (f *fakeSource[T]) DoStreamAny(_ context.Context, _ any) (action.AnyStream, error) {
	return func(yield func(any, error) bool) {
		for _, item := range f.items {
			if !yield(item, nil) {
				return
			}
		}
	}, nil
}

func makeUnary(name string) action.AnyAction {
	return action.New(name, func(_ context.Context, in string) (string, error) {
		return in, nil
	}).Build()
}

func makeOperator(name string) action.NamedOperator {
	return action.NewSimpleOperator(name, func(up iter.Seq2[int, error]) iter.Seq2[int, error] {
		return up
	})
}

type testOpConfig struct {
	Limit int    `json:"limit"`
	Mode  string `json:"mode"`
}

func opWithConfig(name string) action.NamedOperator {
	return action.NewOperator(name, func(cfg testOpConfig) action.StreamOp[int, int] {
		return func(up iter.Seq2[int, error]) iter.Seq2[int, error] { return up }
	})
}

// ── Registry ─────────────────────────────────────────────────────────────────

func TestRegistry_RegisterAndResolve(t *testing.T) {
	t.Parallel()

	r := flow.NewRegistry()
	lib := flow.Library{
		Name:      "demo",
		Actions:   []action.AnyAction{makeUnary("demo.unary")},
		Sources:   []action.AnyStreamAction{&fakeSource[string]{name: "demo.source"}},
		Operators: []action.NamedOperator{makeOperator("demo.op")},
	}
	if err := r.Register(lib); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cases := []struct {
		name string
		want flow.Kind
	}{
		{"demo.unary", flow.KindUnary},
		{"demo.source", flow.KindSource},
		{"demo.op", flow.KindOperator},
	}
	for _, tc := range cases {
		got, ok := r.Resolve(tc.name)
		if !ok {
			t.Errorf("Resolve(%q) not found", tc.name)
			continue
		}
		if got != tc.want {
			t.Errorf("Resolve(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRegistry_DuplicateAcrossLibraries(t *testing.T) {
	t.Parallel()

	r := flow.NewRegistry()
	if err := r.Register(flow.Library{
		Name:    "first",
		Actions: []action.AnyAction{makeUnary("dup")},
	}); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := r.Register(flow.Library{
		Name:    "second",
		Actions: []action.AnyAction{makeUnary("dup")},
	})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistry_ConflictBetweenKinds(t *testing.T) {
	t.Parallel()

	r := flow.NewRegistry()
	if err := r.Register(flow.Library{
		Name:    "a",
		Actions: []action.AnyAction{makeUnary("clash")},
	}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register(flow.Library{
		Name:    "b",
		Sources: []action.AnyStreamAction{&fakeSource[string]{name: "clash"}},
	})
	if err == nil || !strings.Contains(err.Error(), "clash") {
		t.Fatalf("expected kind-conflict error mentioning clash, got: %v", err)
	}
}

func TestRegistry_TransactionalOnConflict(t *testing.T) {
	t.Parallel()

	r := flow.NewRegistry()
	if err := r.Register(flow.Library{
		Name:    "base",
		Actions: []action.AnyAction{makeUnary("existing")},
	}); err != nil {
		t.Fatalf("base register: %v", err)
	}
	before := r.Names()

	err := r.Register(flow.Library{
		Name: "partial",
		Actions: []action.AnyAction{
			makeUnary("fresh"),
			makeUnary("existing"),
		},
	})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	after := r.Names()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("registry mutated on failed Register: before=%v after=%v", before, after)
	}
}

func TestRegistry_NilActionSkipped(t *testing.T) {
	t.Parallel()
	r := flow.NewRegistry()
	lib := flow.Library{
		Name:    "mixed",
		Actions: []action.AnyAction{nil, makeUnary("valid")},
	}
	if err := r.Register(lib); err != nil {
		t.Fatalf("nil action should not error: %v", err)
	}
	if _, ok := r.Get("valid"); !ok {
		t.Error("valid action missing")
	}
}

func TestRegistry_EmptyLibrary(t *testing.T) {
	t.Parallel()
	r := flow.NewRegistry()
	if err := r.Register(flow.Library{Name: "empty"}); err != nil {
		t.Fatalf("empty library: %v", err)
	}
	if got := r.Names(); len(got) != 0 {
		t.Errorf("expected no names, got %v", got)
	}
}

func TestRegistry_NamesSorted(t *testing.T) {
	t.Parallel()
	r := flow.NewRegistry()
	_ = r.Register(flow.Library{
		Name:    "x",
		Actions: []action.AnyAction{makeUnary("zeta"), makeUnary("alpha"), makeUnary("mu")},
	})
	want := []string{"alpha", "mu", "zeta"}
	if got := r.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestRegistry_NilSafe(t *testing.T) {
	t.Parallel()
	var r *flow.Registry
	if _, ok := r.Resolve("any"); ok {
		t.Error("nil Resolve should return false")
	}
	if _, ok := r.Get("any"); ok {
		t.Error("nil Get should return false")
	}
	if got := r.Names(); got != nil {
		t.Errorf("nil Names should return nil, got %v", got)
	}
}

// ── Strongly-typed Operator Build & Validation ───────────────────────────────

func TestOperator_BuildWithConfig(t *testing.T) {
	t.Parallel()
	op := opWithConfig("test.op")

	// Valid parameters decode directly into testOpConfig
	streamOp, err := op.Build(map[string]any{"limit": 50, "mode": "strict"})
	if err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
	if streamOp == nil || streamOp.Name() != "test.op" {
		t.Fatalf("invalid stream operator built: %+v", streamOp)
	}
}

func TestOperator_BuildRejectsUnknownField(t *testing.T) {
	t.Parallel()
	op := opWithConfig("test.op")

	// Unknown parameters must be rejected immediately at compile time
	_, err := op.Build(map[string]any{"unknown_param": true})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got: %v", err)
	}
}
