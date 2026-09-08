// nexssp/flow/dsl_saga.go
package flow

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

// CompileSaga parses Arrow DSL with embedded transaction rollbacks.
func CompileSaga(expr string, reg Registry) (*action.Builder[any, any], error) {
	expr = stripOuterParens(strings.TrimSpace(expr))

	// 1. Parallel: "(A & B)" -> Compile each child and execute concurrently
	if strings.Contains(expr, "&") {
		parts := splitTopLevel(expr, '&')
		if len(parts) > 1 {
			routes := make(map[string]action.AnyAction, len(parts))
			for i, p := range parts {
				bld, err := CompileSaga(p, reg)
				if err != nil {
					return nil, err
				}
				routes[fmt.Sprintf("branch_%d", i+1)] = bld.Build()
			}

			parallel := action.ParallelNamed[any]("parallel_sagas", routes)
			return action.New("parallel_sagas_wrap", func(ctx context.Context, req any) (any, error) {
				return parallel.Build().Do(ctx, req)
			}), nil
		}
	}

	// 2. Sequential Pipe: "A -> B" -> Convert to an atomic Saga Node
	if parts := splitTopLevelAll(expr, "->"); len(parts) > 1 {
		steps := make([]nodes.SagaStep, 0, len(parts))
		for _, part := range parts {
			name, params := parseTokenParamsAndRollback(part)

			forwardAct, ok := reg.Get(name)
			if !ok {
				return nil, fmt.Errorf("flow: capability %q not found", name)
			}

			var compensateAct action.AnyAction
			if rollbackName, ok := params["rollback"].(string); ok {
				compensateAct, _ = reg.Get(rollbackName)
			}

			steps = append(steps, nodes.SagaStep{
				NodeID:     name,
				Forward:    forwardAct,
				Compensate: compensateAct,
			})
		}
		return nodes.NewDynamicSaga("saga_pipe", steps), nil
	}

	// 3. Fallback to standard Node resolution
	return resolveActionNode(expr, reg)
}

func parseTokenParamsAndRollback(token string) (string, map[string]any) {
	params := make(map[string]any)
	if start := strings.IndexByte(token, '('); start != -1 {
		if end := strings.LastIndexByte(token, ')'); end > start {
			raw := token[start+1 : end]
			token = strings.TrimSpace(token[:start])
			for _, pair := range strings.Split(raw, ",") {
				if key, val, ok := strings.Cut(strings.TrimSpace(pair), "="); ok {
					params[strings.TrimSpace(key)] = strings.TrimSpace(val)
				}
			}
		}
	}
	return token, params
}
