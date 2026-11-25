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

func TestExpressionBindingIsImmutableBeforeAndAfterPromotion(t *testing.T) {
	ctx := compiler.WithExpressionScope(t.Context(), nil)
	if err := compiler.BindExpressionValue(ctx, "first", 1); err != nil {
		t.Fatalf("bind first value: %v", err)
	}
	if err := compiler.BindExpressionValue(ctx, "first", 2); err == nil {
		t.Fatal("rebound inline expression value successfully")
	}
	if err := compiler.BindExpressionValue(ctx, "second", 2); err != nil {
		t.Fatalf("bind second value: %v", err)
	}
	if err := compiler.BindExpressionValue(ctx, "first", 3); err == nil {
		t.Fatal("rebound map-backed expression value successfully")
	}
	if got, want := compiler.ExpressionBindingNames(ctx), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expression binding names = %#v, want %#v", got, want)
	}
}

func TestExpressionPipelineInputBindingNamesRespectShadowing(t *testing.T) {
	parent := compiler.WithExpressionScope(t.Context(), nil)
	if err := compiler.BindPipelineInputValue(parent, "request", map[string]any{"value": "typed"}); err != nil {
		t.Fatalf("bind typed pipeline input: %v", err)
	}
	if err := compiler.BindPipelineInputValue(parent, "request2", map[string]any{"value": "typed2"}); err != nil {
		t.Fatalf("bind second typed pipeline input: %v", err)
	}
	want := map[string]struct{}{"request": {}, "request2": {}}
	if got := compiler.ExpressionPipelineInputBindingNames(parent); !reflect.DeepEqual(got, want) {
		t.Fatalf("typed pipeline input names = %#v, want %#v", got, want)
	}

	child := compiler.WithChildExpressionScope(parent)
	if got := compiler.ExpressionPipelineInputBindingNames(child); !reflect.DeepEqual(got, want) {
		t.Fatalf("inherited typed pipeline input names = %#v, want %#v", got, want)
	}
	if err := compiler.BindExpressionValue(child, "request", map[string]any{"value": "as-output"}); err != nil {
		t.Fatalf("bind shadowing as output: %v", err)
	}
	if got := compiler.ExpressionPipelineInputBindingNames(child); !reflect.DeepEqual(got, map[string]struct{}{"request2": {}}) {
		t.Fatalf("typed pipeline input names after ordinary binding shadows request = %#v, want request2", got)
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

func TestMergeSymbolNamespacesKeepsCallerEnvironmentIsolated(t *testing.T) {
	input := map[string]any{"root": "typed-input"}
	ctx := compiler.WithExpressionScope(t.Context(), input)
	if err := compiler.BindExpressionValue(ctx, "shadow", "scope-value"); err != nil {
		t.Fatalf("bind expression value: %v", err)
	}
	env := map[string]any{"input": "caller-input", "shadow": "caller-value"}

	merged := compiler.MergeSymbolNamespaces(ctx, env)
	if got := merged["input"]; !reflect.DeepEqual(got, input) {
		t.Fatalf("merged input = %#v, want typed input %#v", got, input)
	}
	if got := merged["shadow"]; got != "scope-value" {
		t.Fatalf("merged shadow = %#v, want lexical scope value", got)
	}
	if got := env["input"]; got != "caller-input" {
		t.Fatalf("caller input was mutated: %#v", env)
	}
	if got := env["shadow"]; got != "caller-value" {
		t.Fatalf("caller binding was mutated: %#v", env)
	}
	merged["new"] = "expression-only"
	if _, exists := env["new"]; exists {
		t.Fatal("merged environment aliases the caller's map")
	}
}
