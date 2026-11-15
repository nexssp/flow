package pipeline_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestPipelineArraySchemaDefaultErrorFixtures(t *testing.T) {
	t.Parallel()
	cfg := pipelineTestConfig(t)
	for _, testCase := range []struct {
		fixture  string
		selector string
	}{
		{fixture: "nflows/array_schema_shared_defaults.nflow", selector: ":schema=[]Item"},
		{fixture: "nflows/array_out_schema_defaults.nflow", selector: ":out-schema=[]Item"},
	} {
		t.Run(testCase.fixture, func(t *testing.T) {
			t.Parallel()
			source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, testCase.fixture)
			if err != nil {
				t.Fatalf("read fixture %q: %v", testCase.fixture, err)
			}
			_, err = runner.Execute(t.Context(), cfg, string(source), testCase.fixture, nil)
			if err == nil {
				t.Fatalf("execute fixture %q: expected response-default compile error", testCase.fixture)
			}
			if !strings.Contains(err.Error(), testCase.selector) {
				t.Errorf("error = %v, want authored selector %q", err, testCase.selector)
			}
			if !strings.Contains(err.Error(), "response schemas must not declare defaults") {
				t.Errorf("error = %v, want response-default diagnostic", err)
			}
		})
	}
}

func TestAtomSchemaBoundarySelectorsRejectPipelineSyntax(t *testing.T) {
	t.Parallel()
	cfg := pipelineTestConfig(t)
	for _, testCase := range []struct {
		fixture  string
		selector string
	}{
		{fixture: "nflows/array_atom_schema_strict.nflow", selector: ":schema=!Item"},
		{fixture: "nflows/array_atom_schema_array.nflow", selector: ":schema=[]Item"},
		{fixture: "nflows/array_atom_request_schema_array.nflow", selector: ":request-schema=[]Item"},
	} {
		t.Run(testCase.fixture, func(t *testing.T) {
			t.Parallel()
			source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, testCase.fixture)
			if err != nil {
				t.Fatalf("read fixture %q: %v", testCase.fixture, err)
			}
			_, err = runner.Execute(t.Context(), cfg, string(source), testCase.fixture, nil)
			if err == nil {
				t.Fatalf("execute fixture %q: expected atom-level schema diagnostic", testCase.fixture)
			}
			if !strings.Contains(err.Error(), testCase.selector) || !strings.Contains(err.Error(), "unknown schema") {
				t.Errorf("error = %v, want atom-level unknown-schema diagnostic for %q", err, testCase.selector)
			}
		})
	}
}

func TestResponseArraySchemaPreservesTypedOutput(t *testing.T) {
	t.Parallel()
	type item struct {
		Name string `json:"name"`
	}
	want := []item{{Name: "Ada"}}
	arrayAction := action.New("test.items", func(_ context.Context, _ any) (any, error) {
		return want, nil
	}).Build()
	bundles := append([]compiler.Bundle(nil), native.Bundles()...)
	bundles = append(bundles, compiler.Bundle{
		ID:        "array-response-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{arrayAction}}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	source := "@schema Item {\n  Name string `json:\"name\" validate:\"required\"`\n}\n" +
		"@pipeline items :response-schema=[]Item\n  test.items\n@end\npipeline.items\n"
	result, err := runner.Execute(t.Context(), cfg, source, "typed-array-response.nflow", nil)
	if err != nil {
		t.Fatalf("execute typed array response: %v", err)
	}
	got, ok := result.Output.([]item)
	if !ok {
		t.Fatalf("output = %T (%#v), want []item", result.Output, result.Output)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("output = %#v, want %#v", got, want)
	}
}

func TestRequestArraySchemaReturnsDefaultedMaps(t *testing.T) {
	t.Parallel()
	type item struct {
		Name string `json:"name"`
	}
	want := []item{{Name: "Ada"}}
	arrayAction := action.New("test.request_items", func(_ context.Context, _ any) (any, error) {
		return want, nil
	}).Build()
	bundles := append([]compiler.Bundle(nil), native.Bundles()...)
	bundles = append(bundles, compiler.Bundle{
		ID:        "array-request-test",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{arrayAction}}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	source := "@schema Item {\n  Name string `json:\"name\" validate:\"required\"`\n  Role string `json:\"role\" default:\"guest\" validate:\"required\"`\n}\n" +
		"@pipeline items :request-schema=[]Item\n  runtime.noop\n@end\ntest.request_items -> pipeline.items\n"
	result, err := runner.Execute(t.Context(), cfg, source, "typed-array-request.nflow", nil)
	if err != nil {
		t.Fatalf("execute typed array request: %v", err)
	}
	got, ok := result.Output.([]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want []any of defaulted maps", result.Output, result.Output)
	}
	if len(got) != 1 {
		t.Fatalf("output = %#v, want one item", got)
	}
	values, ok := got[0].(map[string]any)
	if !ok {
		t.Fatalf("output item = %T (%#v), want map[string]any", got[0], got[0])
	}
	if values["name"] != "Ada" || values["role"] != "guest" {
		t.Fatalf("output item = %#v, want name Ada and defaulted role guest", values)
	}
}
