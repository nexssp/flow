package nodes_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

func TestSupervisor_DynamicChildSpawning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// 1. Predefined Prompt Node
	summarizePrompt := nodes.NewPromptNode(nodes.PromptConfig{
		Name:         "ai.summarize",
		Description:  "Summarizes raw text into executive bullet points",
		SystemPrompt: "You are a concise executive summarizer.",
		UserTemplate: "Summarize: {{ .input }}",
	})

	// 2. Worker Tool
	slackTool := action.New("slack.post", func(_ context.Context, req map[string]any) (string, error) {
		return "posted_to_slack", nil
	}).Build()

	// 3. Registry that child pipelines compile against.
	reg := flow.NewRegistry(summarizePrompt, slackTool)

	// 4. Supervisor reads registry + compiler from context.
	ctx = contracts.WithRegistry(ctx, reg)
	if pc, ok := any(reg).(contracts.PipelineCompiler); ok {
		ctx = contracts.WithCompiler(ctx, pc)
	}

	supervisorNode := nodes.NewSupervisorNode("main.supervisor")
	execAct := action.Dynamic(supervisorNode)

	req := nodes.SupervisorReq{
		Tasks: []nodes.ChildTask{
			{
				ID:      "task_1",
				DSL:     "ai.summarize -> { channel: '#general', text: rendered_user } -> slack.post",
				Payload: "System load spike detected on node-01",
			},
			{
				ID:      "task_2",
				DSL:     "ai.summarize",
				Payload: "Database index maintenance completed",
			},
		},
	}

	res, err := execAct.Build().Do(ctx, req)
	if err != nil {
		t.Fatalf("supervisor execution failed: %v", err)
	}

	supRes, ok := res.(nodes.SupervisorRes)
	if !ok {
		t.Fatalf("unexpected output type: %T", res)
	}

	if supRes.Total != 2 || supRes.Succeeded != 2 {
		t.Fatalf("expected 2 successful child tasks, got succeeded=%d failed=%d",
			supRes.Succeeded, supRes.Failed)
	}
}
