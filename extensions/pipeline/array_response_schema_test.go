package pipeline_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/nexssp/flow/compiler"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
	"github.com/nexssp/kernel/action"
)

func TestResponseArraySchemaFixtures(t *testing.T) {
	t.Parallel()
	cfg := pipelineTestConfig(t)
	for _, fixture := range []string{
		"nflows/array_response_schema.nflow",
		"nflows/array_response_scalar.nflow",
		"nflows/array_response_wrong_shape.nflow",
		"nflows/array_response_invalid_item.nflow",
		"nflows/array_response_invalid_scalar.nflow",
		"nflows/array_response_strict_item.nflow",
	} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, fixture)
			if err != nil {
				t.Fatalf("read fixture %q: %v", fixture, err)
			}
			if _, err := runner.Execute(t.Context(), cfg, string(source), fixture, nil); err != nil {
				t.Fatalf("execute fixture %q: %v", fixture, err)
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
