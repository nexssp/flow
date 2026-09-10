package nodes

import (
	"context"
	"fmt"

	"github.com/nexssp/kernel/action"
)

type SagaStep struct {
	NodeID     string
	Forward    action.AnyAction
	Compensate action.AnyAction
}

func NewDynamicSaga(name string, steps []SagaStep) *action.Builder[any, any] {
	return action.New(name, func(ctx context.Context, input any) (any, error) {
		executedSteps := make([]SagaStep, 0, len(steps))
		currentOutput := input

		for _, step := range steps {
			exec, ok := step.Forward.(action.Executable)
			if !ok {
				return nil, fmt.Errorf("flow: saga node %s is not executable", step.NodeID)
			}

			out, err := exec.ExecuteDecoded(ctx, func(target any) error {
				return decodePayload(currentOutput, target)
			})
			if err != nil {
				// Compensate only steps that previously completed successfully in LIFO order
				for i := len(executedSteps) - 1; i >= 0; i-- {
					compStep := executedSteps[i]
					if compStep.Compensate != nil {
						if compExec, compOk := compStep.Compensate.(action.Executable); compOk {
							_, _ = compExec.ExecuteDecoded(ctx, func(target any) error {
								return decodePayload(input, target)
							})
						}
					}
				}

				return nil, fmt.Errorf("saga aborted at %s: %w", step.NodeID, err)
			}

			executedSteps = append(executedSteps, step)
			currentOutput = out
		}

		return currentOutput, nil
	})
}
