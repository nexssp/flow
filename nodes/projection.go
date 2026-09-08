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

func NewProjectionAction(code string) (*action.BuiltAction[any, any], error) {
	program, err := expr.Compile(code, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("flow: invalid projection expression %q: %w", code, err)
	}

	nodeID := fmt.Sprintf("projection_%d", projectionCounter.Add(1))

	return action.New(nodeID, func(ctx context.Context, input any) (any, error) {
		out, err := expr.Run(program, normalizeProjectionEnv(input))
		if err != nil {
			return nil, fmt.Errorf("flow: projection failed: %w", err)
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

	rv := reflect.ValueOf(input)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return rv.Interface()
	}

	typ := rv.Type()
	out := make(map[string]any, typ.NumField())

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		name := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			if parts := strings.Split(tag, ","); parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		}

		out[name] = rv.Field(i).Interface()
	}

	return out
}

var _ = vm.Program{}
