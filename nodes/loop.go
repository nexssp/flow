package nodes

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

var loopCounter atomic.Int64

func NewLoopAction(
	bodyAction action.AnyAction,
	untilCondition string,
	maxTurns int,
) (*action.BuiltAction[any, any], error) {
	if maxTurns <= 0 {
		maxTurns = 15
	}

	program, err := expr.Compile(untilCondition, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("flow: invalid loop until condition %q: %w", untilCondition, err)
	}

	nodeID := fmt.Sprintf("loop_%d", loopCounter.Add(1))

	executable, ok := bodyAction.(action.Executable)
	if !ok {
		return nil, fmt.Errorf("flow: loop body action %q is not executable", bodyAction.Describe().Name)
	}

	return action.New(nodeID, func(ctx context.Context, input any) (any, error) {
		currentInput := input

		for turn := 1; turn <= maxTurns; turn++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			output, execErr := executable.ExecuteDecoded(ctx, func(target any) error {
				return decodePayload(currentInput, target)
			})
			if execErr != nil {
				return nil, execErr
			}

			env := normalizeProjectionEnv(output)

			satisfied, evalErr := evalLoopCondition(program, env)
			if evalErr != nil {
				return nil, evalErr
			}

			if satisfied {
				return output, nil
			}

			currentInput = output
		}

		return nil, xerr.Timeout(fmt.Sprintf("flow: autonomous loop exceeded max turns (%d)", maxTurns))
	}).
		Internal().
		Build(), nil
}

func evalLoopCondition(program *vm.Program, env any) (bool, error) {
	out, err := expr.Run(program, env)
	if err != nil {
		return false, fmt.Errorf("flow: loop condition run failed: %w", err)
	}

	if b, ok := out.(bool); ok {
		return b, nil
	}

	return false, nil
}
