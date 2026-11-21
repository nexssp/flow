package projection

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

// Request is one projection evaluation.
type Request struct {
	Raw   string
	Input any
}

// Evaluator converts a projection body into a value. NewExprEvaluator
// is the production implementation; DefaultEvaluator is a pass-through
// used only in isolated tests.
type Evaluator func(context.Context, Request) (any, error)

// DefaultEvaluator returns the input unchanged.
func DefaultEvaluator(_ context.Context, req Request) (any, error) {
	return req.Input, nil
}

// NewExprEvaluator compiles each projection body once and caches it.
// The transformation rules are documented in buildProgramSource and
// preprocessDots.
func NewExprEvaluator() Evaluator {
	cache := newProgramCache()
	viewCache := newExpressionViewTypeCache()
	return func(ctx context.Context, req Request) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		body := strings.TrimSpace(req.Raw)
		if body == "" {
			return map[string]any{}, nil
		}

		env := normalizeExpressionEnvironmentWithCache(compiler.MergeSymbolNamespaces(ctx, buildEnv(req.Input)), viewCache)
		visibleNames := projectionRuntimeVisibleNames(ctx)
		for name := range env {
			visibleNames[name] = struct{}{}
		}
		program, err := cache.compile(body, visibleNames)
		if err != nil {
			return nil, err
		}
		if conflictErr := rejectRuntimeProjectionBindingConflict(ctx, body, req.Input, viewCache); conflictErr != nil {
			return nil, conflictErr
		}

		output, err := expr.Run(program, env)
		if err != nil {
			return nil, xerr.BadRequest(fmt.Sprintf("%s: %v", projectionEvaluationMessage(req.Raw), err))
		}
		return output, nil
	}
}

type programCache struct {
	entries sync.Map
}

func newProgramCache() *programCache { return &programCache{} }

func projectionRuntimeVisibleNames(ctx context.Context) map[string]struct{} {
	names := map[string]struct{}{
		expressionInputName: {},
		expressionRootName:  {},
		expressionStateName: {},
	}
	for _, name := range compiler.ExpressionBindingNames(ctx) {
		names[name] = struct{}{}
	}
	for name := range compiler.LocalSymbolsFrom(ctx) {
		names[name] = struct{}{}
	}
	for _, namespace := range compiler.Namespaces() {
		names[namespace.Name] = struct{}{}
	}
	return names
}

func (c *programCache) compile(raw string, visibleNames ...map[string]struct{}) (*vm.Program, error) {
	var names map[string]struct{}
	if len(visibleNames) != 0 {
		names = visibleNames[0]
	}
	return c.compileWithOptions(raw, names, nil, expr.AllowUndefinedVariables(), spreadMergeOption)
}

func (c *programCache) compileWithOptions(
	raw string,
	visibleNames map[string]struct{},
	sourceRewrite func(string) string,
	options ...expr.Option,
) (*vm.Program, error) {
	key := projectionProgramCacheKey(raw, visibleNames)
	if cached, ok := c.entries.Load(key); ok {
		if program, ok := cached.(*vm.Program); ok {
			return program, nil
		}
		// Cache corrupted or wrong type; fall through and recompile.
		c.entries.Delete(key)
	}

	source, err := buildProgramSourceWithVisibleNames(raw, visibleNames)
	if err != nil {
		return nil, xerr.Validation(fmt.Sprintf("projection %q: %v", strings.TrimSpace(raw), err))
	}
	if sourceRewrite != nil {
		source = sourceRewrite(source)
	}
	program, err := compiler.CompileExpression(source, options...)
	if err != nil {
		return nil, xerr.Validation(fmt.Sprintf("projection %q: %v", strings.TrimSpace(raw), err))
	}

	c.entries.Store(key, program)
	return program, nil
}

func projectionProgramCacheKey(raw string, visibleNames map[string]struct{}) string {
	if len(visibleNames) == 0 {
		return raw
	}
	names := make([]string, 0, len(visibleNames))
	for name := range visibleNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return raw + "\x00" + strings.Join(names, "\x00")
}

// buildEnv marks the input root for one-pass view normalization.
func buildEnv(input any) map[string]any {
	return map[string]any{expressionRootName: expressionEnvironmentRoot{value: input}}
}
