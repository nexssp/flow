package compiler

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

type expressionScope struct {
	parent      *expressionScope
	input       any
	bindings    map[string]any
	unavailable map[string]struct{}
}

var expressionScopeKey = xctx.NewKey[*expressionScope]("flow.expression.scope")

// ValidateExpressionBindingName checks the lexical identifier accepted by
// `as name`. The request and Flow's established root/state names are reserved.
func ValidateExpressionBindingName(name string) error {
	if name == "" || !isExpressionIdentStart(name[0]) {
		return fmt.Errorf("invalid pipeline binding name %q", name)
	}
	for i := 1; i < len(name); i++ {
		if !isExpressionIdentContinue(name[i]) {
			return fmt.Errorf("invalid pipeline binding name %q: use one identifier", name)
		}
	}
	switch name {
	case "input", "__root__", "__state__", "as", "true", literalFalse, "nil", "null":
		return fmt.Errorf("pipeline binding name %q is reserved", name)
	}
	return nil
}

func isExpressionIdentStart(char byte) bool {
	return char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func isExpressionIdentContinue(char byte) bool {
	return isExpressionIdentStart(char) || char >= '0' && char <= '9'
}

// WithExpressionScope starts a new pipeline-local expression scope. The
// original request is exposed to expressions as `input`; this context scope is
// separate from the payload flowing between actions.
func WithExpressionScope(ctx context.Context, input any) context.Context {
	return expressionScopeKey.With(ctx, &expressionScope{
		input:    input,
		bindings: make(map[string]any),
	})
}

// WithChildExpressionScope creates an isolated lexical child. It inherits the
// original request and names from its parent but does not export new bindings.
func WithChildExpressionScope(ctx context.Context) context.Context {
	parent, _ := expressionScopeKey.From(ctx)
	child := &expressionScope{parent: parent, bindings: make(map[string]any)}
	if parent != nil {
		child.input = parent.input
	}
	return expressionScopeKey.With(ctx, child)
}

// HasExpressionScope reports whether ctx carries a Flow pipeline expression
// scope.
func HasExpressionScope(ctx context.Context) bool {
	scope, ok := expressionScopeKey.From(ctx)
	return ok && scope != nil
}

// ExpressionBindingNames returns the visible lexical binding names without
// exposing their values. The result includes inherited parent-scope names.
func ExpressionBindingNames(ctx context.Context) []string {
	scope, _ := expressionScopeKey.From(ctx)
	if scope == nil {
		return nil
	}
	names := make(map[string]struct{})
	for current := scope; current != nil; current = current.parent {
		for name := range current.bindings {
			names[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// BindExpressionValue adds an immutable name to the current lexical scope.
// Names may shadow a parent frame but may not be rebound within one frame.
func BindExpressionValue(ctx context.Context, name string, value any) error {
	scope, ok := expressionScopeKey.From(ctx)
	if !ok || scope == nil {
		return fmt.Errorf("pipeline binding %q has no expression scope", name)
	}
	if _, exists := scope.bindings[name]; exists {
		return fmt.Errorf("pipeline binding %q is immutable and already bound", name)
	}
	scope.bindings[name] = value
	return nil
}

// MarkExpressionBindingUnavailable records a name whose action failed before
// producing the value that its `as` binding would expose.
func MarkExpressionBindingUnavailable(ctx context.Context, name string) {
	scope, ok := expressionScopeKey.From(ctx)
	if !ok || scope == nil {
		return
	}
	if scope.unavailable == nil {
		scope.unavailable = make(map[string]struct{})
	}
	scope.unavailable[name] = struct{}{}
}

// ExpressionUnavailableBindingNames returns failed `as` bindings visible from
// the current lexical scope, including its parents.
func ExpressionUnavailableBindingNames(ctx context.Context) []string {
	scope, _ := expressionScopeKey.From(ctx)
	if scope == nil {
		return nil
	}
	var names map[string]struct{}
	for current := scope; current != nil; current = current.parent {
		if len(current.unavailable) == 0 {
			continue
		}
		if names == nil {
			names = make(map[string]struct{})
		}
		for name := range current.unavailable {
			names[name] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// WithExpressionScopeAction resets expression bindings for one declared
// pipeline invocation while preserving the request passed to that pipeline.
func WithExpressionScopeAction(inner action.AnyAction) action.AnyAction {
	return action.Dynamic(inner).UseFirst(func(next action.Fn[any, any]) action.Fn[any, any] {
		return func(ctx context.Context, request any) (any, error) {
			return next(WithExpressionScope(ctx, request), request)
		}
	}).Build()
}

// WithChildExpressionScopeAction runs an action in an isolated child frame.
// When invoked directly outside a runner, the request becomes the root input.
func WithChildExpressionScopeAction(inner action.AnyAction) action.AnyAction {
	return action.Dynamic(inner).UseFirst(func(next action.Fn[any, any]) action.Fn[any, any] {
		return func(ctx context.Context, request any) (any, error) {
			if !HasExpressionScope(ctx) {
				ctx = WithExpressionScope(ctx, request)
			}
			return next(WithChildExpressionScope(ctx), request)
		}
	}).Build()
}

func mergeExpressionScope(ctx context.Context, env map[string]any) map[string]any {
	scope, _ := expressionScopeKey.From(ctx)
	if scope == nil {
		return env
	}
	var frames []*expressionScope
	for current := scope; current != nil; current = current.parent {
		frames = append(frames, current)
	}
	out := make(map[string]any, len(env)+len(frames)*2)
	maps.Copy(out, env)
	for _, frame := range slices.Backward(frames) {
		out["input"] = frame.input
		maps.Copy(out, frame.bindings)
	}
	return out
}
