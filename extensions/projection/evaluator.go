package projection

import (
	"context"
	"fmt"
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

		program, err := cache.compile(body)
		if err != nil {
			return nil, err
		}

		env := normalizeExpressionEnvironmentWithCache(compiler.MergeSymbolNamespaces(ctx, buildEnv(req.Input)), viewCache)
		output, err := expr.Run(program, env)
		if err != nil {
			return nil, xerr.BadRequest(fmt.Sprintf("projection %q: %v", strings.TrimSpace(req.Raw), err))
		}
		return output, nil
	}
}

type programCache struct {
	entries sync.Map
}

func newProgramCache() *programCache { return &programCache{} }

func (c *programCache) compile(raw string) (*vm.Program, error) {
	if cached, ok := c.entries.Load(raw); ok {
		if program, ok := cached.(*vm.Program); ok {
			return program, nil
		}
		// Cache corrupted or wrong type; fall through and recompile.
		c.entries.Delete(raw)
	}

	source, err := buildProgramSource(raw)
	if err != nil {
		return nil, xerr.Validation(fmt.Sprintf("projection %q: %v", strings.TrimSpace(raw), err))
	}
	program, err := compiler.CompileExpression(source,
		expr.AllowUndefinedVariables(),
		spreadMergeOption,
	)
	if err != nil {
		return nil, xerr.Validation(fmt.Sprintf("projection %q: %v", strings.TrimSpace(raw), err))
	}

	c.entries.Store(raw, program)
	return program, nil
}

// buildEnv marks the input root for one-pass view normalization.
func buildEnv(input any) map[string]any {
	return map[string]any{expressionRootName: expressionEnvironmentRoot{value: input}}
}
