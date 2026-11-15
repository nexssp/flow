package pipeline_test

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/runner"
)

func TestTypedSymbolsSurvivePipelineInAndOutSchemaBoundaries(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, "nflows/typed_schema_boundaries.nflow")
	if err != nil {
		t.Fatalf("read typed schema boundary fixture: %v", err)
	}
	source := string(fixture)
	cfg := pipelineTestConfig(t)
	file := "typed-schema-boundaries.nflow"

	result, err := runner.Execute(t.Context(), cfg, source, file, map[string]any{})
	if err != nil {
		t.Fatalf("execute valid request with typed defaults and output validation: %v", err)
	}
	values, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("result output = %T (%#v), want map[string]any", result.Output, result.Output)
	}
	if values["status"] != 201 || values["timeout"] != 5*time.Second {
		t.Fatalf("effective request/response = %#v, want status 201 and timeout 5s", values)
	}

	_, err = runner.Execute(t.Context(), cfg, source, file, map[string]any{
		"status": 202, "timeout": 5 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "TypedRequest") {
		t.Fatalf("invalid input error = %v, want TypedRequest boundary failure", err)
	}

	invalidResponseSource := strings.Replace(source, "pipeline.typed_boundary\n", "pipeline.typed_invalid_response\n", 1)
	_, err = runner.Execute(t.Context(), cfg, invalidResponseSource, file, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "TypedResponse") {
		t.Fatalf("invalid output error = %v, want TypedResponse boundary failure", err)
	}
}

func TestPipelineInSchemaSeedsTypedBodyAnalysis(t *testing.T) {
	t.Parallel()
	cfg := pipelineTestConfig(t)
	const schema = "@schema ToolPayload {\n  Name string `json:\"name\"`\n}\n" +
		"@schema Request {\n  Tool ToolPayload `json:\"tool\"`\n  Name string `json:\"name\"`\n  Task string `json:\"task\"`\n}\n"
	tests := []struct {
		name      string
		pipeline  string
		wantError bool
	}{
		{
			name: "valid field through an identity binding",
			pipeline: "@pipeline typed :in-schema=!Request\n" +
				"  noop as request\n" +
				"  -> { value: request.task }\n" +
				"@end\n",
		},
		{
			name: "used bare name collides even when both paths are valid",
			pipeline: "@pipeline typed :in-schema=!Request\n" +
				"  noop as tool\n" +
				"  -> { value: tool.name }\n" +
				"@end\n",
			wantError: true,
		},
		{
			name: "explicit current-input field access",
			pipeline: "@pipeline typed :in-schema=!Request\n" +
				"  noop as tool\n" +
				"  -> { value: .tool.name }\n" +
				"@end\n",
		},
		{
			name: "unused name overlap is allowed",
			pipeline: "@pipeline typed :in-schema=!Request\n" +
				"  noop as tool\n" +
				"  -> { value: \"unchanged\" }\n" +
				"@end\n",
		},
		{
			name: "unknown input stays dynamic without a schema",
			pipeline: "@pipeline typed\n" +
				"  noop as task\n" +
				"  -> { value: task.name }\n" +
				"@end\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := schema + tc.pipeline + "pipeline.typed\n"
			_, err := runner.Compile(t.Context(), cfg, source, "in-schema-type-analysis.nflow")
			if !tc.wantError && err != nil {
				t.Fatalf("Compile() error = %v, want success", err)
			}
			if tc.wantError && (err == nil || !strings.Contains(err.Error(), "ambiguous bare identifier \"tool\"")) {
				t.Fatalf("Compile() error = %v, want static input/binding collision rejection", err)
			}
		})
	}
}
