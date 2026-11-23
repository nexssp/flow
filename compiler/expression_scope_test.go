package compiler_test

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestCachedExpressionInputViewSharedAcrossChildScopes(t *testing.T) {
	parent := compiler.WithExpressionScope(t.Context(), "request-1")
	child := compiler.WithChildExpressionScope(parent)
	var builds atomic.Int32
	build := func(input any) any {
		builds.Add(1)
		return &struct{ input any }{input: input}
	}
	first, ok := compiler.CachedExpressionInputView(parent, "test.request-view", build)
	if !ok {
		t.Fatal("parent expression scope did not return a cached input view")
	}
	second, ok := compiler.CachedExpressionInputView(child, "test.request-view", build)
	if !ok || first != second {
		t.Fatalf("child view = (%v, %t), want the same cached view as parent", second, ok)
	}
	if got := builds.Load(); got != 1 {
		t.Fatalf("view builder ran %d times, want once", got)
	}

	const workers = 16
	start := make(chan struct{})
	views := make(chan any, workers)
	var callers sync.WaitGroup
	for range workers {
		callers.Go(func() {
			<-start
			view, cached := compiler.CachedExpressionInputView(child, "test.concurrent-view", build)
			if !cached {
				t.Error("child expression scope did not return a concurrent cached view")
				return
			}
			views <- view
		})
	}
	close(start)
	callers.Wait()
	close(views)
	var concurrentView any
	for view := range views {
		if concurrentView == nil {
			concurrentView = view
		} else if view != concurrentView {
			t.Fatal("concurrent callers received different views for the same cache key")
		}
	}
	if got := builds.Load(); got != 2 {
		t.Fatalf("view builder ran %d times after concurrent access, want once per key", got)
	}

	separateScope := compiler.WithExpressionScope(t.Context(), "request-2")
	third, ok := compiler.CachedExpressionInputView(separateScope, "test.request-view", build)
	if !ok || third == first {
		t.Fatalf("separate scope view = (%v, %t), want an independent cached view", third, ok)
	}
	if got := builds.Load(); got != 3 {
		t.Fatalf("view builder ran %d times across independent scopes, want 3", got)
	}
}

func TestCachedExpressionInputViewRebuildsForMutableOriginalInput(t *testing.T) {
	input := map[string]any{"request_id": "before"}
	ctx := compiler.WithExpressionScope(t.Context(), input)
	build := func(value any) any {
		fields, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("input type = %T, want map[string]any", value)
		}
		return map[string]any{"request_id": fields["request_id"]}
	}
	first, ok := compiler.CachedExpressionInputView(ctx, "test.mutable-request-view", build)
	firstFields, isMap := first.(map[string]any)
	if !ok || !isMap || firstFields["request_id"] != "before" {
		t.Fatalf("first view = (%#v, %t), want original value", first, ok)
	}
	input["request_id"] = "after"
	second, ok := compiler.CachedExpressionInputView(ctx, "test.mutable-request-view", build)
	secondFields, isMap := second.(map[string]any)
	if !ok || !isMap || secondFields["request_id"] != "after" {
		t.Fatalf("second view = (%#v, %t), want updated original value", second, ok)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatal("mutable original input reused a stale cached view")
	}
}

func TestExpressionBindingNamesIncludesVisibleParentAndChildBindings(t *testing.T) {
	parent := compiler.WithExpressionScope(t.Context(), nil)
	if err := compiler.BindExpressionValue(parent, "parent_value", "not exposed"); err != nil {
		t.Fatalf("bind parent value: %v", err)
	}
	if err := compiler.BindExpressionValue(parent, "shadowed", "parent"); err != nil {
		t.Fatalf("bind parent shadowed value: %v", err)
	}
	child := compiler.WithChildExpressionScope(parent)
	if err := compiler.BindExpressionValue(child, "child_value", "not exposed"); err != nil {
		t.Fatalf("bind child value: %v", err)
	}
	if err := compiler.BindExpressionValue(child, "shadowed", "child"); err != nil {
		t.Fatalf("bind child shadowed value: %v", err)
	}

	got := compiler.ExpressionBindingNames(child)
	want := []string{"child_value", "parent_value", "shadowed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpressionBindingNames() = %#v, want %#v", got, want)
	}
}

func TestExpressionPipelineInputBindingNamesRespectShadowing(t *testing.T) {
	parent := compiler.WithExpressionScope(t.Context(), nil)
	if err := compiler.BindPipelineInputValue(parent, "request", map[string]any{"value": "typed"}); err != nil {
		t.Fatalf("bind typed pipeline input: %v", err)
	}
	if got := compiler.ExpressionPipelineInputBindingNames(parent); !reflect.DeepEqual(got, map[string]struct{}{"request": {}}) {
		t.Fatalf("typed pipeline input names = %#v, want request", got)
	}

	child := compiler.WithChildExpressionScope(parent)
	if got := compiler.ExpressionPipelineInputBindingNames(child); !reflect.DeepEqual(got, map[string]struct{}{"request": {}}) {
		t.Fatalf("inherited typed pipeline input names = %#v, want request", got)
	}
	if err := compiler.BindExpressionValue(child, "request", map[string]any{"value": "as-output"}); err != nil {
		t.Fatalf("bind shadowing as output: %v", err)
	}
	if got := compiler.ExpressionPipelineInputBindingNames(child); len(got) != 0 {
		t.Fatalf("typed pipeline input names after ordinary binding shadows it = %#v, want empty", got)
	}
}

func TestValidateExpressionBindingNameRejectsParserLiterals(t *testing.T) {
	for _, name := range []string{"true", "false", "nil", "null"} {
		t.Run(name, func(t *testing.T) {
			if err := compiler.ValidateExpressionBindingName(name); err == nil {
				t.Fatalf("ValidateExpressionBindingName(%q) succeeded; want reserved-name error", name)
			}
		})
	}
}
