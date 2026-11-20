package runner

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/pipeline"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
)

func TestExecutePreparedPipelineSchemaBoundary(t *testing.T) {
	var calls atomic.Int32
	capture := action.New("test.capture", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return input, nil
	}).Build()
	testBundle := compiler.Bundle{
		ID:        "schema-boundary-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{capture}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
		testBundle,
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `@schema Request {
  Name   string   ` + "`json:\"name\" default:\"schema-name\" validate:\"required\"`" + `
  Depth  int      ` + "`json:\"depth\" default:\"1\" validate:\"gte=1\"`" + `
  Labels []string ` + "`json:\"labels\" validate:\"dive,required\"`" + `
}

@pipeline handle
  :request-schema=Request
  test.capture
@end
`
	prepared, err := Preflight(t.Context(), cfg, source, "schema-boundary.nflow")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	payload := map[string]any{
		"name":   "explicit-name",
		"labels": []string{"one", "two"},
	}
	result, err := ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.handle",
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("ExecutePrepared valid request: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T, want map[string]any", result.Output)
	}
	if got := output["name"]; got != "explicit-name" {
		t.Fatalf("name = %#v, want explicit value", got)
	}
	if got := output["depth"]; got != 1 {
		t.Fatalf("depth = %#v, want default 1", got)
	}
	labels, ok := output["labels"].([]string)
	if !ok {
		t.Fatalf("labels type = %T, want []string", output["labels"])
	}
	if len(labels) != 2 || labels[0] != "one" || labels[1] != "two" {
		t.Fatalf("labels = %#v, want original typed slice", labels)
	}
	if _, exists := payload["depth"]; exists {
		t.Fatal("schema defaults mutated the caller's payload")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pipeline body calls = %d, want 1", got)
	}

	invalid := map[string]any{
		"depth":  0,
		"labels": []string{"valid"},
	}
	_, err = ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name:    "pipeline.handle",
		Payload: invalid,
	})
	if err == nil {
		t.Fatal("ExecutePrepared invalid request succeeded")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pipeline body ran for invalid request: calls = %d, want 1", got)
	}
}

func TestExecutePipelineCallRequestSchemaAppliesDefaultsBeforeBody(t *testing.T) {
	var calls atomic.Int32
	capture := action.New("test.capture", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return input, nil
	}).Build()
	testBundle := compiler.Bundle{
		ID:        "schema-call-boundary-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{capture}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
		testBundle,
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `@schema Request {
  Depth int ` + "`json:\"depth\" default:\"1\" validate:\"gte=1\"`" + `
}

@pipeline handle
  :request-schema=Request
  test.capture
@end

pipeline.handle
`
	result, err := Execute(t.Context(), cfg, source, "schema-call-boundary.nflow", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T, want map[string]any", result.Output)
	}
	if got := output["depth"]; got != 1 {
		t.Fatalf("depth = %#v, want default 1", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pipeline body calls = %d, want 1", got)
	}
}

func TestExecutePipelineRequestSchemaRequiredFieldMarker(t *testing.T) {
	var calls atomic.Int32
	capture := action.New("test.capture", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return input, nil
	}).Build()
	testBundle := compiler.Bundle{
		ID:        "schema-required-marker-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{capture}}},
	}
	cfg, err := BuildConfig([]compiler.Bundle{
		schema.Bundle(nil),
		pipeline.Bundle(nil),
		runtimeext.Bundle(nil),
		testBundle,
	})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `@schema Customer {
  Name! string ` + "`json:\"name\"`" + `
}
@schema Request {
  Customer! Customer ` + "`json:\"customer\"`" + `
  Labels! []string ` + "`json:\"labels\" validate:\"min=1,max=15,dive,required\"`" + `
  Note string ` + "`json:\"note\" validate:\"min=1\"`" + `
}
@pipeline handle
  :request-schema=Request
  test.capture
@end
`
	prepared, err := Preflight(t.Context(), cfg, source, "required-marker-boundary.nflow")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	valid := map[string]any{
		"customer": map[string]any{"name": ""},
		"labels":   []string{"present"},
	}
	result, err := ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name: "pipeline.handle", Payload: valid,
	})
	if err != nil {
		t.Fatalf("ExecutePrepared valid request: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T, want map[string]any", result.Output)
	}
	if _, exists := output["note"]; exists {
		t.Fatal("missing optional field with validate tag was treated as required")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pipeline body calls = %d, want 1", got)
	}

	invalid := map[string]any{
		"customer": map[string]any{},
		"labels":   []string{"present"},
	}
	_, err = ExecutePrepared(t.Context(), cfg, prepared, EntryPoint{
		Name: "pipeline.handle", Payload: invalid,
	})
	if err == nil || !strings.Contains(err.Error(), `field "name" is required`) {
		t.Fatalf("missing nested Customer.name error = %v, want required-field failure", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pipeline body ran for invalid request: calls = %d, want 1", got)
	}
}
