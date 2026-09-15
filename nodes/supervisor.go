package nodes

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"golang.org/x/sync/errgroup"
)

type ChildTask struct {
	ID        string `json:"id"`
	DSL       string `json:"dsl"`
	Payload   any    `json:"payload"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

type SupervisorReq struct {
	Tasks []ChildTask `json:"tasks" validate:"required"`
}

type ChildResult struct {
	TaskID   string `json:"task_id"`
	Output   any    `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"duration_ms"`
}

type SupervisorRes struct {
	Total     int           `json:"total"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Results   []ChildResult `json:"results"`
}

// NewSupervisorNode builds a supervisor that compiles and runs child
// pipelines on the fly. The compiler is obtained from the execution
// context, so the node works inside any flow without constructor args.
func NewSupervisorNode(name string) action.AnyAction {
	return action.New(name, func(ctx context.Context, req SupervisorReq) (SupervisorRes, error) {
		if len(req.Tasks) == 0 {
			return SupervisorRes{}, xerr.BadRequest("supervisor: no child tasks provided")
		}

		compiler := contracts.CompilerFromContext(ctx)
		if compiler == nil {
			return SupervisorRes{}, xerr.Internal("supervisor: no pipeline compiler in context")
		}

		results := make([]ChildResult, len(req.Tasks))
		g, gCtx := errgroup.WithContext(ctx)

		for i, t := range req.Tasks {
			idx := i
			task := t

			g.Go(func() error {
				start := time.Now()
				resSlot := &results[idx]
				resSlot.TaskID = task.ID

				defer func() {
					if r := recover(); r != nil {
						resSlot.Error = fmt.Sprintf("panic isolated: %v", r)
						resSlot.Duration = time.Since(start).Milliseconds()
					}
				}()

				timeout := 30 * time.Second
				if task.TimeoutMS > 0 {
					timeout = time.Duration(task.TimeoutMS) * time.Millisecond
				}

				childCtx, cancel := context.WithTimeout(gCtx, timeout)
				defer cancel()

				childPipeline, compileErr := compiler.CompilePipeline(task.DSL)
				if compileErr != nil {
					resSlot.Error = fmt.Sprintf("compile error: %v", compileErr)
					resSlot.Duration = time.Since(start).Milliseconds()

					return nil
				}

				out, execErr := childPipeline.ExecuteDecoded(childCtx, func(target any) error {
					return decodePayload(task.Payload, target)
				})

				resSlot.Duration = time.Since(start).Milliseconds()
				if execErr != nil {
					resSlot.Error = execErr.Error()
				} else {
					resSlot.Output = out
				}

				return nil
			})
		}

		_ = g.Wait()

		var succeeded, failed int

		for _, r := range results {
			if r.Error != "" {
				failed++
			} else {
				succeeded++
			}
		}

		return SupervisorRes{
			Total:     len(results),
			Succeeded: succeeded,
			Failed:    failed,
			Results:   results,
		}, nil
	}).
		Description("Main orchestrator node that dynamically spawns and supervises child pipelines").
		Build()
}
