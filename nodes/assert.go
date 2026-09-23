package nodes

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

var assertNodeCounter atomic.Uint64

// NewAssertAction compiles an `assert(cond, msg)` guard. The condition
// is rewritten through PreprocessDotNotation so that leading-dot state
// references (`.game_over == false`, `.take >= 1`) compile identically
// to projections and loop conditions.
//
// On a passing assertion the input payload is returned unchanged, so an
// assert can be inserted anywhere in a pipeline without altering the
// data flow.
func NewAssertAction(condition, message string) (*action.BuiltAction[any, any], error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return nil, fmt.Errorf("flow: assert condition cannot be empty")
	}
	if message == "" {
		message = "assertion failed: " + condition
	}

	compiledCondition, _ := PreprocessDotNotation(condition)

	compiledProgram, err := expr.Compile(compiledCondition, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("flow: invalid assert expression %q: %w", condition, err)
	}

	nodeIdentifier := fmt.Sprintf("assert_%d", assertNodeCounter.Add(1))

	return action.New(nodeIdentifier, func(_ context.Context, input any) (any, error) {
		environment := NormalizeEnv(input)

		evaluationResult, runError := expr.Run(compiledProgram, environment)
		if runError != nil {
			return nil, xerr.Internal("assert evaluation failed", runError)
		}

		passed, isBoolean := evaluationResult.(bool)
		if !isBoolean {
			return nil, xerr.Validation(fmt.Sprintf("assert expression must return bool, got %T", evaluationResult))
		}
		if !passed {
			return nil, xerr.Validation(message)
		}

		return input, nil
	}).
		Internal().
		Build(), nil
}
