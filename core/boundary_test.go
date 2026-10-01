package core

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

// fakeStreamSource returns a StreamAction that yields the given items in
// order and then completes. Used to exercise boundaries without pulling
// in extensions/fs (which would create an import cycle).
func fakeStreamSource(name string, items ...any) action.AnyStreamAction {
	return action.NewStream(name, func(_ context.Context, _ struct{}) (iter.Seq2[any, error], error) {
		return func(yield func(any, error) bool) {
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
		}, nil
	})
}

func requirePanic(tb testing.TB, hint string, fn func()) {
	tb.Helper()

	defer func() {
		recovered := recover()
		if recovered == nil {
			tb.Fatalf("expected panic containing %q, got none", hint)
		}
		if !strings.Contains(fmt.Sprint(recovered), hint) {
			tb.Fatalf("expected panic containing %q, got %v", hint, recovered)
		}
	}()

	fn()
}

// ── Registry contract ────────────────────────────────────────────────

func TestBoundary_CollectIsRegistered(t *testing.T) {
	t.Parallel()

	spec, ok := BoundaryByName("collect")
	ktest.RequireCondition(t, ok, "collect boundary must be registered in init()")
	ktest.RequireEqual(t, spec.Name, "collect")
	ktest.RequireCondition(t, spec.Consume != nil, "collect boundary must have a Consume function")
	ktest.RequireCondition(t, spec.Description != "", "collect boundary must have a description")
}

func TestBoundary_ByNameUnknown(t *testing.T) {
	t.Parallel()

	_, ok := BoundaryByName("not-a-boundary")
	ktest.RequireCondition(t, !ok, "unknown name must return false")
}

func TestBoundary_NamedBoundariesSorted(t *testing.T) {
	t.Parallel()

	specs := NamedBoundaries()
	ktest.RequireCondition(t, len(specs) >= 1, "at least one boundary must be registered")

	for i := 1; i < len(specs); i++ {
		ktest.RequireCondition(t, specs[i-1].Name < specs[i].Name,
			"NamedBoundaries must be sorted by Name; got %q before %q",
			specs[i-1].Name, specs[i].Name)
	}
}

func TestBoundary_RegisterBoundaryPanicsOnEmptyName(t *testing.T) {
	t.Parallel()

	requirePanic(t, "empty Name", func() {
		RegisterBoundary(BoundarySpec{
			Consume: func(_ action.AnyStreamAction) *action.Builder[any, any] {
				return action.New("x", func(_ context.Context, in any) (any, error) { return in, nil })
			},
		})
	})
}

func TestBoundary_RegisterBoundaryPanicsOnNilConsume(t *testing.T) {
	t.Parallel()

	requirePanic(t, "nil Consume", func() {
		RegisterBoundary(BoundarySpec{Name: "nope"})
	})
}

func TestBoundary_RegisterBoundaryPanicsOnDuplicate(t *testing.T) {
	t.Parallel()

	requirePanic(t, "duplicate boundary collect", func() {
		RegisterBoundary(BoundarySpec{
			Name: "collect",
			Consume: func(_ action.AnyStreamAction) *action.Builder[any, any] {
				return action.New("x", func(_ context.Context, in any) (any, error) { return in, nil })
			},
		})
	})
}

// ── Integration with the parser and compiler ─────────────────────────

func TestBoundary_CollectEndToEnd(t *testing.T) {
	t.Parallel()

	resolver, err := NewDynamicResolver(action.Library{
		Name: "test",
		Sources: []action.AnyStreamAction{
			fakeStreamSource("test.source", "a", "b", "c"),
		},
	})
	ktest.RequireNoError(t, err)

	ast, err := NewParserWithFileOffset(
		context.Background(),
		testTable(),
		DefaultPrimaryExtensions(),
		`test.source -> collect`,
		"",
		0,
	).Parse()
	ktest.RequireNoError(t, err)

	program, err := Build(context.Background(), resolver, nil, ast)
	ktest.RequireNoError(t, err)

	output, err := action.InvokeAny(context.Background(), program, nil)
	ktest.RequireNoError(t, err)

	items, ok := output.([]any)
	ktest.RequireCondition(t, ok, "collect must produce []any, got %T", output)
	ktest.RequireEqual(t, items, []any{"a", "b", "c"})
}

func TestBoundary_ImplicitCollectEndToEnd(t *testing.T) {
	t.Parallel()

	resolver, err := NewDynamicResolver(action.Library{
		Name:    "test",
		Sources: []action.AnyStreamAction{fakeStreamSource("test.source", "a", "b")},
		Operators: []action.NamedOperator{
			action.NewSimpleOperator[any, any]("test.noop", func(up iter.Seq2[any, error]) iter.Seq2[any, error] {
				return up
			}),
		},
	})
	ktest.RequireNoError(t, err)

	ast, err := NewParserWithFileOffset(
		context.Background(),
		testTable(),
		DefaultPrimaryExtensions(),
		`test.source -> test.noop`,
		"",
		0,
	).Parse()
	ktest.RequireNoError(t, err)

	program, err := Build(context.Background(), resolver, nil, ast)
	ktest.RequireNoError(t, err)

	output, err := action.InvokeAny(context.Background(), program, nil)
	ktest.RequireNoError(t, err)
	items, ok := output.([]any)
	ktest.RequireCondition(t, ok, "implicit collect must produce []any, got %T", output)
	ktest.RequireEqual(t, items, []any{"a", "b"})
}

func TestBoundary_CollectWithDownstreamAtom(t *testing.T) {
	t.Parallel()

	resolver, err := NewDynamicResolver(action.Library{
		Name: "test",
		Actions: []action.AnyAction{
			action.New("count", func(_ context.Context, in []any) (int, error) {
				return len(in), nil
			}).Build(),
		},
		Sources: []action.AnyStreamAction{
			fakeStreamSource("test.source", "x", "y"),
		},
	})
	ktest.RequireNoError(t, err)

	ast, err := NewParserWithFileOffset(
		context.Background(),
		testTable(),
		DefaultPrimaryExtensions(),
		`test.source -> collect -> count`,
		"",
		0,
	).Parse()
	ktest.RequireNoError(t, err)

	program, err := Build(context.Background(), resolver, nil, ast)
	ktest.RequireNoError(t, err)

	output, err := action.InvokeAny(context.Background(), program, nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, output, 2)
}

func TestBoundary_CollectRequiresUpstream(t *testing.T) {
	t.Parallel()

	resolver, err := NewDynamicResolver(action.Library{Name: "test"})
	ktest.RequireNoError(t, err)

	// `collect` alone — no stream source on its left — must be rejected
	// by the compiler, not silently produce an empty pipeline.
	ast, err := NewParserWithFileOffset(
		context.Background(),
		testTable(),
		DefaultPrimaryExtensions(),
		`collect`,
		"",
		0,
	).Parse()
	ktest.RequireNoError(t, err)

	_, err = Build(context.Background(), resolver, nil, ast)
	ktest.RequireErrorContains(t, err, "collect")
}

func TestBoundary_OnlyOneBoundaryPerPipe(t *testing.T) {
	t.Parallel()

	resolver, err := NewDynamicResolver(action.Library{
		Name: "test",
		Sources: []action.AnyStreamAction{
			fakeStreamSource("test.source", "x"),
		},
	})
	ktest.RequireNoError(t, err)

	ast, err := NewParserWithFileOffset(
		context.Background(),
		testTable(),
		DefaultPrimaryExtensions(),
		`test.source -> collect -> collect`,
		"",
		0,
	).Parse()
	ktest.RequireNoError(t, err)

	_, err = Build(context.Background(), resolver, nil, ast)
	ktest.RequireErrorContains(t, err, "only one stream boundary")
}
