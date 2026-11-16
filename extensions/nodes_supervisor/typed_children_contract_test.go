package nodes_supervisor_test

import (
	"io/fs"
	"testing"

	nodessupervisor "github.com/nexssp/flow/extensions/nodes_supervisor"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestSupervisorChildrenNFlowUsesTypedSchema(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	fixture, err := fs.ReadFile(nodessupervisor.Bundle(nil).Fixtures, "nflows/supervisor_children.nflow")
	if err != nil {
		t.Fatalf("read supervisor fixture: %v", err)
	}
	input := map[string]any{
		"tasks": []any{
			map[string]any{"id": "num", "dsl": "const @{ value: 42 }", "payload": map[string]any{}},
			map[string]any{"id": "boom", "dsl": "fail", "payload": map[string]any{}},
			map[string]any{"id": "echo", "dsl": "with @{ echoed: true }", "payload": map[string]any{"greeting": "hi"}},
		},
	}
	result, err := runner.Execute(t.Context(), cfg, string(fixture), "supervisor_children.nflow", input)
	if err != nil {
		t.Fatalf("execute typed supervisor fixture: %v", err)
	}
	assertions, ok := nflowStringList(result.Meta["asserts"])
	if !ok || len(assertions) == 0 {
		t.Fatal("supervisor fixture did not expose its @assert directives")
	}
	if failures := runner.EvaluateAssertions(result.Output, assertions); len(failures) != 0 {
		t.Fatalf("supervisor fixture assertions failed: %v", failures)
	}
}

func nflowStringList(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return values, true
	case []any:
		result := make([]string, len(values))
		for index, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			result[index] = text
		}
		return result, true
	default:
		return nil, false
	}
}
