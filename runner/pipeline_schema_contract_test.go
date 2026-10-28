package runner

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/pipeline"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
)

func TestPipelineRequestResponseSchemaContract(t *testing.T) {
	var calls atomic.Int32
	handler := action.New("test.contract", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		request, ok := input.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input = %T, want map[string]any", input)
		}
		message := request["name"]
		if request["mode"] == "invalid-response" {
			message = 42
		}
		return map[string]any{
			"message": message,
			"state":   map[string]any{"depth": request["depth"]},
		}, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "pipeline-schema-contract-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{handler}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
		bundle,
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `@schema Request {
  Name  string ` + "`json:\"name\" default:\"Guest\" validate:\"required\"`" + `
  Depth int    ` + "`json:\"depth\" default:\"1\" validate:\"gte=1\"`" + `
}
@schema Response {
  Message  string ` + "`json:\"message\" validate:\"required\"`" + `
  Optional string ` + "`json:\"optional\"`" + `
}
@pipeline handle
  :request-schema=Request
  :response-schema=Response
  test.contract
@end
`
	prepared, err := Preflight(t.Context(), cfg, source, "pipeline-schema-contract.nflow")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	request := map[string]any{"request_extra": true}
	result, err := ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.handle",
		Payload: request,
	})
	if err != nil {
		t.Fatalf("ExecutePrepared valid request: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T, want map[string]any", result.Output)
	}
	if got := output["message"]; got != "Guest" {
		t.Fatalf("message = %#v, want request default Guest", got)
	}
	state, ok := output["state"].(map[string]any)
	if !ok || state["depth"] != 1 {
		t.Fatalf("state = %#v, want preserved extra state with default depth", output["state"])
	}
	if _, exists := output["optional"]; exists {
		t.Fatal("response boundary inserted an absent optional field")
	}
	if _, exists := request["name"]; exists {
		t.Fatal("request defaults mutated the caller's payload")
	}
	if request["request_extra"] != true {
		t.Fatal("request validation changed the caller's extra field")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pipeline body calls = %d, want 1", got)
	}

	_, err = ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.handle",
		Payload: map[string]any{"depth": 0},
	})
	if err == nil {
		t.Fatal("invalid request succeeded")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pipeline body ran for invalid request: calls = %d, want 1", got)
	}

	_, err = ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.handle",
		Payload: map[string]any{"mode": "invalid-response"},
	})
	if err == nil {
		t.Fatal("invalid response succeeded")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("pipeline body calls = %d after invalid response, want 2", got)
	}
}

func TestPipelineResponseSchemaDefaultsRejectedBeforeExecution(t *testing.T) {
	var calls atomic.Int32
	respond := action.New("test.response_default_guard", func(context.Context, any) (any, error) {
		calls.Add(1)
		return map[string]any{"name": "Ada"}, nil
	}).Build()
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
		{ID: "response-default-guard-test", Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{respond}}}},
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	for _, testCase := range []struct {
		name       string
		defaultTag string
	}{
		{name: "empty tag", defaultTag: `default:""`},
		{name: "nonempty tag", defaultTag: `default:"response-default"`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := `@schema Response {
  Name string ` + "`json:\"name\" " + testCase.defaultTag + "`" + `
}
@pipeline handle
  :response-schema=Response
  test.response_default_guard
@end
`
			_, err := Execute(t.Context(), cfg, source, "response-default-guard.nflow", map[string]any{})
			if err == nil {
				t.Fatal("response schema with a default compiled and executed")
			}
			if !strings.Contains(err.Error(), "response schemas must not declare defaults") {
				t.Fatalf("error = %v, want clear response-default compile diagnostic", err)
			}
			if !strings.Contains(err.Error(), `field "name"`) {
				t.Fatalf("error = %v, want offending field name", err)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("response handler ran before schema rejection: calls = %d", got)
			}
		})
	}
}

func TestPipelineSchemaStrictnessIsPerBoundary(t *testing.T) {
	var calls atomic.Int32
	handler := action.New("test.strict_contract", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		request, ok := input.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input = %T, want map[string]any", input)
		}
		return map[string]any{
			"message": request["name"],
			"extra":   "kept unless response is strict",
		}, nil
	}).Build()
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
		{ID: "strict-pipeline-schema-test", Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{handler}}}},
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `@schema Request {
  Name string ` + "`json:\"name\" validate:\"required\"`" + `
}
@schema Response {
  Message string ` + "`json:\"message\" validate:\"required\"`" + `
}
@pipeline strict_request
  :request-schema=!Request
  test.strict_contract
@end
@pipeline strict_response
  :response-schema=!Response
  test.strict_contract
@end
`
	prepared, err := Preflight(t.Context(), cfg, source, "strict-pipeline-schema.nflow")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	result, err := ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.strict_request",
		Payload: map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("strict request with declared fields: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok || output["extra"] != "kept unless response is strict" {
		t.Fatalf("strict request unexpectedly made response strict: %#v", result.Output)
	}

	_, err = ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.strict_request",
		Payload: map[string]any{"name": "Ada", "unexpected": true},
	})
	if err == nil {
		t.Fatal("strict request accepted an undeclared input field")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("body ran after strict request rejection: calls = %d, want 1", got)
	}

	_, err = ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.strict_response",
		Payload: map[string]any{"name": "Grace", "request_extra": true},
	})
	if err == nil {
		t.Fatal("strict response accepted an undeclared output field")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("strict response rejected the request before running the body: calls = %d, want 2", got)
	}
}

func TestPipelineSchemaLegacyHeaderAliasRejected(t *testing.T) {
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	source := `@schema Request {
  Name string ` + "`json:\"name\"`" + `
}
@pipeline old_style:schema=Request
  runtime.noop
@end
`
	_, err = Compile(t.Context(), cfg, source, "legacy-pipeline-schema.nflow")
	if err == nil {
		t.Fatal("legacy :schema pipeline header was accepted")
	}
	if !strings.Contains(err.Error(), ":schema is no longer supported") {
		t.Fatalf("error = %v, want removed-alias diagnostic", err)
	}
}

type typedSchemaDetails struct {
	Count int `json:"count"`
}

type typedSchemaEmbedded struct {
	Embedded string `json:"embedded"`
}

type typedSchemaResult struct {
	typedSchemaEmbedded
	Message string             `json:"message"`
	Details typedSchemaDetails `json:"details"`
	Extra   string             `json:"extra"`
	Ignored string             `json:"-"`
}

func TestPipelineResponseSchemaPreservesTypedResultAndChecksJSONFields(t *testing.T) {
	var responseCalls atomic.Int32
	original := &typedSchemaResult{
		Embedded: "promoted",
		Message:  "typed result",
		Details:  typedSchemaDetails{Count: 7},
		Extra:    "must remain visible to strict checking",
		Ignored:  "not part of JSON",
	}
	produce := action.New("test.typed_response", func(context.Context, any) (any, error) {
		responseCalls.Add(1)
		return original, nil
	}).Build()
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
		{ID: "typed-response-schema-test", Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{produce}}}},
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `@schema Response {
  Message string  ` + "`json:\"message\" validate:\"required\"`" + `
  Meta    Details ` + "`json:\"details\"`" + `
}
@pipeline permissive
  :response-schema=Response
  test.typed_response
@end
@pipeline strict
  :response-schema=!Response
  test.typed_response
@end
`
	prepared, err := Preflight(t.Context(), cfg, source, "typed-response-schema.nflow")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	result, err := ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{Name: "pipeline.permissive", Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("permissive response schema: %v", err)
	}
	got, ok := result.Output.(*typedSchemaResult)
	if !ok {
		t.Fatalf("output = %T, want *typedSchemaResult", result.Output)
	}
	if got != original {
		t.Fatalf("output pointer = %p, want original pointer %p", got, original)
	}
	if got.Extra != original.Extra || got.Embedded != original.Embedded {
		t.Fatalf("typed output lost exported JSON fields: %#v", got)
	}

	_, err = ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{Name: "pipeline.strict", Payload: map[string]any{}})
	if err == nil {
		t.Fatal("strict response schema accepted exported JSON fields outside its schema")
	}
	if !strings.Contains(err.Error(), "unknown fields") || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("strict response error = %v, want unknown JSON field extra", err)
	}
	if got := responseCalls.Load(); got != 2 {
		t.Fatalf("response action calls = %d, want 2", got)
	}
}
