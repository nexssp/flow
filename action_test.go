package flow_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func TestGraph_ExecuteAction_DualInputModes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	actStepA := action.New("step.a", func(_ context.Context, _ map[string]any) (string, error) {
		return "alpha", nil
	}).Build()

	actStepB := action.New("step.b", func(_ context.Context, _ map[string]any) (string, error) {
		return "beta", nil
	}).Build()

	registry := action.MustNewRegistry(action.Of(actStepA, actStepB))
	compiler := flow.NewCompiler(registry)
	execAction := flow.NewExecuteAction(compiler)

	// Mode 1: Compact Arrow DSL
	resDSL, err := flow.Execute(ctx, execAction, flow.GraphExecReq{
		DSL: "step.a -> step.b",
	})
	if err != nil {
		t.Fatalf("DSL execution failed: %v", err)
	}

	if resDSL.LayersRun != 2 {
		t.Errorf("expected 2 layers, got %d", resDSL.LayersRun)
	}

	// Mode 2: Declarative YAML Manifest
	yamlManifest := `
apiVersion: nexss.ai/v1
kind: Graph
metadata:
  name: yaml-pipeline
  version: "1.0.0"
nodes:
  - id: node_1
    capability: step.a
  - id: node_2
    capability: step.b
edges:
  - from: node_1
    to: node_2
`

	resYAML, err := flow.Execute(ctx, execAction, flow.GraphExecReq{
		YAML: yamlManifest,
	})
	if err != nil {
		t.Fatalf("YAML execution failed: %v", err)
	}

	if resYAML.GraphName != "yaml-pipeline" || resYAML.LayersRun != 2 {
		t.Errorf("unexpected YAML execution output: %+v", resYAML)
	}
}
