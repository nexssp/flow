// path: nexssp/flow/nodes/saga.go
package nodes

import (
	"context"
	"fmt"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type SagaStep struct {
	NodeID     string
	Forward    action.AnyAction
	Compensate action.AnyAction
}

// NewDynamicSaga chains steps: each step's output feeds the next step's
// input. If a step fails, completed steps are compensated in LIFO order.
//
// This is deliberately distinct from kernel/action.NewSaga, which runs
// every step against the same input. Here we need transformation
// chaining with rollback, which the flow DSL authors expect from
// `A -> B -> C` syntax.
func NewDynamicSaga(name string, steps []SagaStep) *action.Builder[any, any] {
	// Validate once at construction, not per call.
	execs := make([]action.Executable, len(steps))

	undos := make([]action.Executable, len(steps))
	for i, s := range steps {
		if s.Forward == nil {
			panic(fmt.Sprintf("saga %q: step %q has nil Forward", name, s.NodeID))
		}

		ex, ok := s.Forward.(action.Executable)
		if !ok {
			panic(fmt.Sprintf("saga %q: step %q Forward is not executable", name, s.NodeID))
		}

		execs[i] = ex

		if s.Compensate != nil {
			cx, ok := s.Compensate.(action.Executable)
			if !ok {
				panic(fmt.Sprintf("saga %q: step %q Compensate is not executable", name, s.NodeID))
			}

			undos[i] = cx
		}
	}

	return action.New(name, func(ctx context.Context, input any) (any, error) {
		completed := make([]int, 0, len(steps))
		current := input

		for i, s := range steps {
			out, err := execs[i].ExecuteDecoded(ctx, func(target any) error {
				return decodePayload(current, target)
			})
			if err != nil {
				// Roll back completed steps in reverse order. Each
				// compensation receives the original saga input, which
				// is what the compensating action (e.g. cancel_flight)
				// typically needs to identify the resource.
				var rollbackErr error

				for j := len(completed) - 1; j >= 0; j-- {
					idx := completed[j]
					if undos[idx] == nil {
						continue
					}

					if _, cerr := undos[idx].ExecuteDecoded(ctx, func(target any) error {
						return decodePayload(input, target)
					}); cerr != nil && rollbackErr == nil {
						rollbackErr = cerr
					}
				}

				if rollbackErr != nil {
					return nil, xerr.Internal(
						fmt.Sprintf("saga %q failed at step %q; rollback also failed", name, s.NodeID),
						err,
					)
				}

				return nil, fmt.Errorf("saga %q failed at step %q: %w", name, s.NodeID, err)
			}

			completed = append(completed, i)
			current = out
		}

		return current, nil
	}).Tag("saga", "dynamic")
}
