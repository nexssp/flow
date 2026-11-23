package nodes_supervisor

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"golang.org/x/sync/errgroup"

	"github.com/nexssp/flow/compiler/contracts"
)

const (
	childDefaultTimeout = 30 * time.Second
	// Keep supervisor requests consistent with the bounded fan-out item limit.
	maxChildTasks       = 100_000
	maxChildConcurrency = 256
	maxChildTimeoutMS   = int64(math.MaxInt64) / int64(time.Millisecond)
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

// Supervisor compiles and runs each task's DSL with bounded concurrency.
// Children are isolated from panics, errors, and timeouts; results preserve
// input order. The compiler is read from the execution context.
var Supervisor = action.New("supervisor.run", func(ctx context.Context, req SupervisorReq) (SupervisorRes, error) {
	if len(req.Tasks) == 0 {
		return SupervisorRes{}, xerr.BadRequest("supervisor: no child tasks provided")
	}
	if len(req.Tasks) > maxChildTasks {
		return SupervisorRes{}, xerr.BadRequest(fmt.Sprintf("supervisor: %d child tasks exceeds limit of %d", len(req.Tasks), maxChildTasks))
	}
	for _, task := range req.Tasks {
		if task.TimeoutMS < 0 || task.TimeoutMS > maxChildTimeoutMS {
			return SupervisorRes{}, xerr.BadRequest("supervisor: timeout_ms is outside the supported range")
		}
	}

	compiler := contracts.CompilerFromContext(ctx)
	if compiler == nil {
		return SupervisorRes{}, xerr.Internal("supervisor: no pipeline compiler in context")
	}

	results := make([]ChildResult, len(req.Tasks))
	group, groupCtx := errgroup.WithContext(ctx)
	slots := make(chan struct{}, maxChildConcurrency)

	for i, task := range req.Tasks {
		acquired := false
		select {
		case slots <- struct{}{}:
			acquired = true
		case <-groupCtx.Done():
		}
		if err := groupCtx.Err(); err != nil {
			if acquired {
				<-slots
			}
			for j := i; j < len(req.Tasks); j++ {
				results[j] = ChildResult{TaskID: req.Tasks[j].ID, Error: err.Error()}
			}
			break
		}
		index, current := i, task
		group.Go(func() error {
			defer func() { <-slots }()
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
	if err := childCtx.Err(); err != nil {
		result.Error = err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}

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
