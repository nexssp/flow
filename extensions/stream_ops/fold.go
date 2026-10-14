package stream_ops

import (
	"iter"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/stream"
	"github.com/nexssp/kernel/xerr"
)

// FoldConfig configures stream.fold. Expression is evaluated once per input
// item with the current accumulator available as acc and the input as item.
type FoldConfig struct {
	Initial    any    `json:"initial"`
	Expression string `json:"expression"`
}

// Fold consumes the complete stream and emits one accumulated value. Its
// expression is compiled once when the operator is built; evaluation remains
// streaming and retains only the accumulator.
func Fold(cfg FoldConfig) action.StreamOp[any, any] {
	expression := strings.TrimSpace(cfg.Expression)
	if expression == "" {
		return foldError(xerr.Validation("stream.fold: expression is required"))
	}

	program, err := expr.Compile(expression, expr.AllowUndefinedVariables())
	if err != nil {
		return foldError(xerr.Validation("stream.fold: invalid expression: " + err.Error()))
	}

	return stream.Reduce[any, any](cfg.Initial, func(acc, item any) (any, error) {
		result, runErr := expr.Run(program, map[string]any{
			"acc":  acc,
			"item": item,
		})
		if runErr != nil {
			return acc, xerr.BadRequest("stream.fold: expression evaluation failed: " + runErr.Error())
		}
		return result, nil
	})
}

func foldError(err error) action.StreamOp[any, any] {
	return func(iter.Seq2[any, error]) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			yield(nil, err)
		}
	}
}
