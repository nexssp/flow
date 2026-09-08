package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type ExecutionTask struct {
	Code      string `json:"code"`
	Feedback  string `json:"feedback,omitempty"`
	IsSuccess bool   `json:"is_success"`
}

func main() {
	ctx := context.Background()
	var iteration int

	runCodeTests := action.New("test.runner", func(_ context.Context, task ExecutionTask) (ExecutionTask, error) {
		iteration++
		fmt.Printf("⚙️ [RUNNER] Executing code (Attempt #%d):\n   %s\n", iteration, strings.ReplaceAll(task.Code, "\n", " "))

		if !strings.Contains(task.Code, "package main") {
			return task, xerr.Internal("missing 'package main' declaration")
		}
		if !strings.Contains(task.Code, "func main") {
			return task, xerr.Internal("missing 'func main()' entrypoint")
		}

		task.IsSuccess = true
		return task, nil
	}).Build()

	aiCorrection := action.New("ai.healer", func(_ context.Context, task ExecutionTask) (ExecutionTask, error) {
		fmt.Printf("🧠 [LLM HEALER] Resolving error: %q\n", task.Feedback)

		if strings.Contains(task.Feedback, "package main") {
			task.Code = "package main\n" + task.Code
		} else if strings.Contains(task.Feedback, "func main") {
			task.Code += "\nfunc main() { println(\"Hello world\") }"
		}
		return task, nil
	}).Build()

	var lastError error
	healerPipeline := runCodeTests.ToBuilder().
		RetryIf(3, action.ConstantBackoff(50*time.Millisecond), func(err error) bool {
			lastError = err
			return true
		}).
		HookRetry(func(c context.Context, req ExecutionTask, attempt int, err error, meta *action.Meta) {
			task := req
			task.Feedback = lastError.Error()

			fixedTask, healErr := aiCorrection.Do(c, task)
			if healErr == nil {
				req.Code = fixedTask.Code
			}
		})

	initialTask := ExecutionTask{
		Code: `import "fmt"`,
	}

	fmt.Println("🚀 Starting reflexion pipeline...")
	finalTask, err := healerPipeline.Build().Do(ctx, initialTask)
	if err != nil {
		fmt.Printf("❌ Failed to resolve errors: %v\n", err)
	} else {
		fmt.Printf("\n✅ Program Repaired Successfully! Final Code:\n%s\n", finalTask.Code)
	}
}
