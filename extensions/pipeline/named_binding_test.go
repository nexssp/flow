package pipeline_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	syntaxext "github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestNamedPipelineBindings_EmbeddedFixture(t *testing.T) {
	const fixturePath = "nflows/named_bindings.nflow"
	source, err := fs.ReadFile(syntaxext.Bundle(nil).Fixtures, fixturePath)
	if err != nil {
		t.Fatalf("read named-binding fixture %q: %v", fixturePath, err)
	}
	result, err := runner.Execute(context.Background(), pipelineTestConfig(t), string(source), "syntax/"+fixturePath, nil)
	if err != nil {
		t.Fatalf("execute named-binding fixture: %v", err)
	}
	output := requireOutputMap(t, result.Output)
	if output["known"] != true || output["choice"] != "approved" || output["original"] != "original request" || output["flowing"] != true {
		t.Fatalf("fixture output = %#v, want all named-binding outputs", output)
	}
}

func TestNamedPipelineBindings_AIOutputExamples(t *testing.T) {
	cfg := pipelineTestConfig(t)
	registerAIBindingTestActions(t, cfg)

	estimateSource := `@pipeline estimate_result
  ai.cost.estimate as estimate
  -> { known: estimate.Known, original: input.query, flowing: .Known, root_known: __root__.Known, state_known: __state__.Known }
@end
{ query: "original request" } -> pipeline.estimate_result
`
	estimate, err := runner.Execute(context.Background(), cfg, estimateSource, "named-binding-estimate.nflow", nil)
	if err != nil {
		t.Fatalf("execute estimate binding example: %v", err)
	}
	estimateOutput := requireOutputMap(t, estimate.Output)
	if estimateOutput["known"] != true || estimateOutput["flowing"] != true || estimateOutput["root_known"] != true || estimateOutput["state_known"] != true || estimateOutput["original"] != "original request" {
		t.Fatalf("estimate output = %#v, want known/flowing/root/state true and original request", estimateOutput)
	}
	if _, leaked := estimateOutput["estimate"]; leaked {
		t.Fatalf("binding was rewritten into the pipeline result: %#v", estimateOutput)
	}

	cfg = pipelineTestConfig(t)
	registerAIBindingTestActions(t, cfg)
	decideSource := `@pipeline decision_result
  ai.decide as decision
  -> { choice: decision.Answers[0].Choice, original: input.prompt, flowing: .status }
@end
{ prompt: "original prompt", status: "received" } -> pipeline.decision_result
`
	decision, err := runner.Execute(context.Background(), cfg, decideSource, "named-binding-decision.nflow", nil)
	if err != nil {
		t.Fatalf("execute decision binding example: %v", err)
	}
	decisionOutput := requireOutputMap(t, decision.Output)
	if decisionOutput["choice"] != "approved" || decisionOutput["original"] != "original prompt" || decisionOutput["flowing"] != "received" {
		t.Fatalf("decision output = %#v, want choice approved and original request fields", decisionOutput)
	}
	if _, leaked := decisionOutput["decision"]; leaked {
		t.Fatalf("binding was rewritten into the pipeline result: %#v", decisionOutput)
	}
}

func TestNamedPipelineBindings_DuplicateNamesFailDuringCompilation(t *testing.T) {
	cfg := pipelineTestConfig(t)
	source := `@pipeline duplicate_names
  runtime.const @{ value: { Known: true } } as estimate
  -> runtime.const @{ value: { Known: false } } as estimate
@end
pipeline.duplicate_names
`
	if _, err := runner.Compile(context.Background(), cfg, source, "duplicate-bindings.nflow"); err == nil || !strings.Contains(err.Error(), `duplicate pipeline binding "estimate"`) {
		t.Fatalf("compile error = %v, want duplicate pipeline binding diagnostic", err)
	}
}

func TestNamedPipelineBindings_DoNotEscapeDeclaredPipeline(t *testing.T) {
	cfg := pipelineTestConfig(t)
	source := `@pipeline private_binding
  runtime.const @{ value: { Known: true } } as estimate
  -> { known: estimate.Known }
@end
{} -> pipeline.private_binding -> { leaked: estimate.Known }
`
	if _, err := runner.Execute(context.Background(), cfg, source, "pipeline-binding-scope.nflow", nil); err == nil || !strings.Contains(err.Error(), "estimate") {
		t.Fatalf("execution error = %v, want the declared pipeline's estimate name to be unavailable outside", err)
	}
}

func TestNamedPipelineBindings_LoopScopeAndErrorRoute(t *testing.T) {
	cfg := pipelineTestConfig(t)
	loopSource := `{ n: 0 } as seed
-> loop( { n: .n + 1 } as step -> { n: step.n, initial: seed.n } ) until( .n >= 2 )
`
	loopResult, err := runner.Execute(context.Background(), cfg, loopSource, "loop-binding-scope.nflow", nil)
	if err != nil {
		t.Fatalf("execute loop binding example: %v", err)
	}
	loopOutput := requireOutputMap(t, loopResult.Output)
	if loopOutput["n"] != int64(2) && loopOutput["n"] != 2 {
		t.Fatalf("loop output n = %#v, want 2", loopOutput["n"])
	}
	if loopOutput["initial"] != int64(0) && loopOutput["initial"] != 0 {
		t.Fatalf("loop output initial = %#v, want inherited seed value 0", loopOutput["initial"])
	}

	errorSource := `{ Known: true } as estimate
-> runtime.fail @{ kind: "Internal", message: "expected failure" } ? on_error ({ recovered: estimate.Known })
`
	errorResult, err := runner.Execute(context.Background(), cfg, errorSource, "error-binding-scope.nflow", nil)
	if err != nil {
		t.Fatalf("execute error-route binding example: %v", err)
	}
	errorOutput := requireOutputMap(t, errorResult.Output)
	if errorOutput["recovered"] != true {
		t.Fatalf("error-route output = %#v, want recovered true from prior binding", errorOutput)
	}
}

func registerAIBindingTestActions(t *testing.T, cfg runner.Config) {
	t.Helper()
	resolver, ok := cfg.Resolver.(*compiler.DynamicResolver)
	if !ok {
		t.Fatalf("runner resolver type = %T, want *compiler.DynamicResolver", cfg.Resolver)
	}
	actions := []action.AnyAction{
		action.New("ai.cost.estimate", func(context.Context, any) (any, error) {
			return map[string]any{"Known": true}, nil
		}).Build(),
		action.New("ai.decide", func(context.Context, any) (any, error) {
			return map[string]any{"Answers": []any{map[string]any{"Choice": "approved"}}, "status": "received"}, nil
		}).Build(),
	}
	if err := resolver.Mount(action.Library{Name: "named-binding-test-actions", Actions: actions}); err != nil {
		t.Fatalf("mount named-binding test actions: %v", err)
	}
}

func requireOutputMap(t *testing.T, output any) map[string]any {
	t.Helper()
	result, ok := output.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T, want map[string]any (output %#v)", output, output)
	}
	return result
}
