package stream_ops

import (
	"iter"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/stream"
	"github.com/nexssp/kernel/xerr"
)

// ExpressionConfig configures a stream transform using an expression over item.
type ExpressionConfig struct {
	Expression string `json:"expression" validate:"required"`
}

type filterValue struct {
	item any
	keep bool
}

// Map transforms each item by evaluating Expression and delegates iteration,
// upstream-error propagation, and downstream stopping to Kernel's MapE.
func Map(cfg ExpressionConfig) action.StreamOp[any, any] {
	const operation = "stream.map"
	program, err := compileTransform(operation, cfg.Expression)
	if err != nil {
		return transformError(err)
	}
	return stream.MapE[any, any](func(item any) (any, error) {
		return evaluateTransform(operation, program, item)
	})
}

// Filter evaluates Expression as a boolean predicate. Kernel's MapE preserves
// evaluation errors, Filter selects accepted items, and Map restores each item.
func Filter(cfg ExpressionConfig) action.StreamOp[any, any] {
	const operation = "stream.filter"
	program, err := compileTransform(operation, cfg.Expression)
	if err != nil {
		return transformError(err)
	}

	evaluated := stream.MapE[any, filterValue](func(item any) (filterValue, error) {
		result, err := evaluateTransform(operation, program, item)
		if err != nil {
			return filterValue{}, err
		}
		keep, ok := result.(bool)
		if !ok {
			return filterValue{}, xerr.BadRequest(operation + ": expression must return a boolean")
		}
		return filterValue{item: item, keep: keep}, nil
	})
	filtered := stream.Filter[filterValue](func(value filterValue) bool {
		return value.keep
	})
	mapped := stream.Map[filterValue, any](func(value filterValue) any {
		return value.item
	})
	return action.StreamOp[any, any](func(upstream iter.Seq2[any, error]) iter.Seq2[any, error] {
		return mapped(filtered(evaluated(upstream)))
	})
}

// FlatMap evaluates Expression to a []any for each item and expands it through
// Kernel's FlatMap and StreamFromSlice, preserving order and downstream stops.
func FlatMap(cfg ExpressionConfig) action.StreamOp[any, any] {
	const operation = "stream.flatMap"
	program, err := compileTransform(operation, cfg.Expression)
	if err != nil {
		return transformError(err)
	}

	mapped := stream.MapE[any, []any](func(item any) ([]any, error) {
		result, err := evaluateTransform(operation, program, item)
		if err != nil {
			return nil, err
		}
		values, ok := result.([]any)
		if !ok {
			return nil, xerr.BadRequest(operation + ": expression must return an array")
		}
		return values, nil
	})
	flatMapped := stream.FlatMap[[]any, any](action.StreamFromSlice[any])
	return action.StreamOp[any, any](func(upstream iter.Seq2[any, error]) iter.Seq2[any, error] {
		return flatMapped(mapped(upstream))
	})
}

func compileTransform(operation, source string) (*vm.Program, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, xerr.Validation(operation + ": expression is required")
	}
	program, err := expr.Compile(source, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, xerr.Validation(operation + ": invalid expression: " + err.Error())
	}
	return program, nil
}

func evaluateTransform(operation string, program *vm.Program, item any) (any, error) {
	result, err := expr.Run(program, map[string]any{"item": item})
	if err != nil {
		return nil, xerr.BadRequest(operation + ": expression evaluation failed: " + err.Error())
	}
	return result, nil
}

func transformError(err error) action.StreamOp[any, any] {
	return func(iter.Seq2[any, error]) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			yield(nil, err)
		}
	}
}
