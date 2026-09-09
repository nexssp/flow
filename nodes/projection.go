package nodes

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/action"
)

var projectionCounter atomic.Int64

// NewProjectionAction compiles inline data shaping { key: expr } using precompiled expr bytecode.
func NewProjectionAction(code string) (*action.BuiltAction[any, any], error) {
	code = strings.TrimSpace(code)
	// expr-lang requires enclosing braces { ... } to parse object map literals
	if !strings.HasPrefix(code, "{") || !strings.HasSuffix(code, "}") {
		code = "{" + code + "}"
	}

	program, err := expr.Compile(code, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("flow: invalid projection expression %q: %w", code, err)
	}

	nodeID := fmt.Sprintf("projection_%d", projectionCounter.Add(1))

	return action.New(nodeID, func(ctx context.Context, input any) (any, error) {
		env := normalizeProjectionEnv(input)
		out, runErr := expr.Run(program, env)
		if runErr != nil {
			return nil, fmt.Errorf("flow: projection failed: %w", runErr)
		}
		return out, nil
	}).
		Internal().
		Build(), nil
}

func normalizeProjectionEnv(input any) any {
	if input == nil {
		return nil
	}

	// ⚡ Fast path: if already a map, pass directly to expr without reflection
	if m, ok := input.(map[string]any); ok {
		return m
	}

	rv := reflect.ValueOf(input)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return input
	}

	typ := rv.Type()
	out := make(map[string]any, typ.NumField()*2)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		val := rv.Field(i).Interface()
		out[field.Name] = val

		// Also register snake_case / json tag alias so expressions can use either
		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				out[parts[0]] = val
			}
		}
	}

	return out
}

var _ = vm.Program{}
