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
