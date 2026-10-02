package nodes_supervisor

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"golang.org/x/sync/errgroup"

	"github.com/nexssp/flow/contracts"
)

const (
	childDefaultTimeout = 30 * time.Second
)

// ChildTask describes one pipeline to compile and run.
type ChildTask struct {
	ID        string `json:"id"`
	DSL       string `json:"dsl"`
	Payload   any    `json:"payload"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

// SupervisorReq is the set of children to run.
type SupervisorReq struct {
	Tasks []ChildTask `json:"tasks" validate:"required"`
}

// ChildResult reports one child's outcome. Error is a plain string so
// the JSON shape stays stable across error kinds.
type ChildResult struct {
	TaskID     string `json:"task_id"`
	Output     any    `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// SupervisorRes aggregates the outcome of every child.
type SupervisorRes struct {
	Total     int           `json:"total"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Results   []ChildResult `json:"results"`
}

// Supervisor compiles and runs each task's DSL. All children run
// concurrently; each child is isolated (panic, error, timeout). The
// compiler is read from the execution context.
var Supervisor = action.New("supervisor.run", func(ctx context.Context, req SupervisorReq) (SupervisorRes, error) {
	if len(req.Tasks) == 0 {
		return SupervisorRes{}, xerr.BadRequest("supervisor: no child tasks provided")
	}

	compiler := contracts.CompilerFromContext(ctx)
	if compiler == nil {
		return SupervisorRes{}, xerr.Internal("supervisor: no pipeline compiler in context")
	}

	results := make([]ChildResult, len(req.Tasks))
	group, groupCtx := errgroup.WithContext(ctx)

	for i, task := range req.Tasks {
		index, current := i, task
		group.Go(func() error {
			results[index] = runChild(groupCtx, compiler, current)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return SupervisorRes{}, err
	}

	var succeeded, failed int
	for i := range results {
		r := &results[i]
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
}).Description("Compile and run named child pipelines concurrently").
	Build()

// runChild executes one child pipeline. Panics and timeouts are
// captured as Error strings; the supervisor never propagates them.
func runChild(ctx context.Context, compiler contracts.PipelineCompiler, task ChildTask) ChildResult {
	started := time.Now()
	result := ChildResult{TaskID: task.ID}

	defer func() {
		if recovered := recover(); recovered != nil {
			result.Error = fmt.Sprintf("panic isolated: %v", recovered)
			result.DurationMs = time.Since(started).Milliseconds()
		}
	}()

	timeout := childDefaultTimeout
	if task.TimeoutMS > 0 {
		timeout = time.Duration(task.TimeoutMS) * time.Millisecond
	}

	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pipeline, err := compiler.CompilePipeline(task.DSL)
	if err != nil {
		result.Error = "compile error: " + err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}

	output, err := pipeline.ExecuteDecoded(childCtx, func(target any) error {
		return action.Assign(target, task.Payload)
	})
	result.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Output = output
	return result
}
