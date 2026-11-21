package projection

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

func TestProjectionStaticAnalysisRejectsUsedInputBindingCollision(t *testing.T) {
	type inputTool struct {
		Name string `json:"name"`
	}
	type request struct {
		Tool inputTool `json:"tool"`
		Name string    `json:"name"`
	}
	bindingType := reflect.TypeFor[struct {
		Name string `json:"name"`
	}]()
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[request]()},
		Bindings: map[string]compiler.TypeDescriptor{
			"tool": {Type: bindingType},
		},
	}

	raw := compiler.StripExprComments("\n  value: (tool.name) # comment before later source")
	_, err := analyzeProjectionTypes(raw, compiler.Position{File: "collision.nflow", Line: 10, Col: 5}, ctx, nil, nil, "")
	if err == nil {
		t.Fatal("static analysis accepted an ambiguous bare input/binding name")
	}
	for _, want := range []string{
		`ambiguous bare identifier "tool"`,
		`input field "tool"`,
		"pipeline `as tool` binding",
		"struct",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic %q does not contain %q", err, want)
		}
	}
	position, ok := compiler.PositionOf(err)
	if !ok {
		t.Fatal("collision diagnostic does not carry a source position")
	}
	wantPosition := compiler.Position{File: "collision.nflow", Line: 11, Col: 11}
	if position != wantPosition {
		t.Fatalf("diagnostic position = %#v, want %#v", position, wantPosition)
	}
}

func TestProjectionStaticAnalysisRejectsUnknownBareFieldAfterHashSelector(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	type request struct {
		Items []item `json:"items"`
	}
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[request]()},
	}
	raw := `filtered: .items | filter(
  #?.name != missing
)`
	_, err := analyzeProjectionTypes(raw, compiler.Position{}, ctx, nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), `no field "missing"`) {
		t.Fatalf("analysis error = %v, want unknown bare field missing after hash selector", err)
	}
}

func TestProjectionStaticAnalysisFindsBareIdentifierAfterHashOperators(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	type request struct {
		Items []item `json:"items"`
	}
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[request]()},
	}
	for _, raw := range []string{
		"filtered: .items | map(\n  # ?? missing\n)",
		"filtered: .items | map(\n  # ? true : missing\n)",
	} {
		_, err := analyzeProjectionTypes(raw, compiler.Position{}, ctx, nil, nil, "")
		if err == nil || !strings.Contains(err.Error(), `no field "missing"`) {
			t.Errorf("analysis error for %q = %v, want unknown bare field missing", raw, err)
		}
	}
}

func TestProjectionStaticAnalysisFindsBindingAfterHashSelector(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	type request struct {
		Items []item `json:"items"`
		Tool  string `json:"tool"`
	}
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[request]()},
		Bindings: map[string]compiler.TypeDescriptor{
			"tool": {Type: reflect.TypeFor[string]()},
		},
	}
	raw := `filtered: .items | filter(
  #?.name == tool
)`
	_, err := analyzeProjectionTypes(raw, compiler.Position{}, ctx, nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), `ambiguous bare identifier "tool"`) {
		t.Fatalf("analysis error = %v, want pipeline-binding conflict for tool", err)
	}
}

func TestProjectionStaticAnalysisAllowsUnambiguousInputAndUnusedOverlap(t *testing.T) {
	type inputTool struct {
		Name string `json:"name"`
	}
	type request struct {
		Tool inputTool `json:"tool"`
	}
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[request]()},
		Bindings: map[string]compiler.TypeDescriptor{
			"tool": {Type: reflect.TypeFor[inputTool]()},
		},
	}

	for _, raw := range []string{
		`value: .tool.name`,               // Leading dot explicitly selects the current payload.
		`tool`,                            // A bare object entry is the documented input shorthand.
		`value: literal`,                  // An unbound value-position name is literal data.
		`value: person.name`,              // An unbound dotted value-position name is literal data.
		`value: "unchanged"`,              // An unused name overlap is harmless.
		`let tool = "local"; value: tool`, // A projection local has its own scope.
	} {
		if _, err := analyzeProjectionTypes(raw, compiler.Position{}, ctx, nil, nil, ""); err != nil {
			t.Errorf("analyzeProjectionTypes(%q) = %v, want success", raw, err)
		}
	}
}

func TestProjectionRuntimeRejectsDynamicInputBindingCollisionWithoutValues(t *testing.T) {
	ctx := compiler.WithExpressionScope(context.Background(), nil)
	if err := compiler.BindExpressionValue(ctx, "tool", map[string]any{"name": "binding-secret"}); err != nil {
		t.Fatalf("bind pipeline value: %v", err)
	}
	input := map[string]any{
		"tool": map[string]any{"name": "payload-secret"},
	}

	_, err := NewExprEvaluator()(ctx, Request{Raw: "value: (tool.name)", Input: input})
	if xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("runtime error = %v, want Conflict", err)
	}
	for _, secret := range []string{"payload-secret", "binding-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("runtime diagnostic leaked payload/binding value %q: %v", secret, err)
		}
	}
	for _, want := range []string{`ambiguous bare identifier "tool"`, "runtime input has a field", "was not evaluated"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("runtime diagnostic %q does not contain %q", err, want)
		}
	}
}

func TestProjectionRuntimeFindsBindingAfterHashSelector(t *testing.T) {
	ctx := compiler.WithExpressionScope(context.Background(), nil)
	if err := compiler.BindExpressionValue(ctx, "tool", "binding"); err != nil {
		t.Fatalf("bind pipeline value: %v", err)
	}
	raw := `filtered: .items | filter(
  #?.name == tool
)`
	input := map[string]any{
		"items": []map[string]any{{"name": "payload"}},
		"tool":  "payload",
	}
	_, err := NewExprEvaluator()(ctx, Request{Raw: raw, Input: input})
	if xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("runtime error = %v, want Conflict for binding reference after hash selector", err)
	}
}

func TestProjectionRuntimeAllowsExplicitInputShorthandAndUnusedOverlap(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want any
	}{
		{name: "qualified current payload", raw: "value: .tool.name", want: map[string]any{"value": "payload"}},
		{name: "object shorthand", raw: "tool", want: map[string]any{"tool": map[string]any{"name": "payload"}}},
		{name: "unused overlap", raw: `value: "ok"`, want: map[string]any{"value": "ok"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := compiler.WithExpressionScope(context.Background(), nil)
			if err := compiler.BindExpressionValue(ctx, "tool", map[string]any{"name": "binding"}); err != nil {
				t.Fatalf("bind pipeline value: %v", err)
			}
			input := map[string]any{"tool": map[string]any{"name": "payload"}}
			got, err := NewExprEvaluator()(ctx, Request{Raw: tc.raw, Input: input})
			if err != nil {
				t.Fatalf("evaluate projection: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("projection output = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestProjectionRuntimeAllowsNoncollidingPipelineBinding(t *testing.T) {
	ctx := compiler.WithExpressionScope(context.Background(), nil)
	if err := compiler.BindExpressionValue(ctx, "tool", map[string]any{"name": "binding"}); err != nil {
		t.Fatalf("bind pipeline value: %v", err)
	}
	got, err := NewExprEvaluator()(ctx, Request{Raw: "value: tool.name", Input: map[string]any{"other": true}})
	if err != nil {
		t.Fatalf("evaluate noncolliding binding: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]any{"value": "binding"}) {
		t.Fatalf("projection output = %#v, want binding value", got)
	}
}

func TestProjectionProgramCacheSeparatesVisibleBindingScopes(t *testing.T) {
	evaluate := NewExprEvaluator()
	request := Request{Raw: "value: tool.name", Input: map[string]any{"other": true}}
	got, err := evaluate(context.Background(), request)
	if err != nil {
		t.Fatalf("evaluate unbound literal: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]any{"value": "tool.name"}) {
		t.Fatalf("unbound projection output = %#v, want literal", got)
	}

	ctx := compiler.WithExpressionScope(context.Background(), nil)
	if err := compiler.BindExpressionValue(ctx, "tool", map[string]any{"name": "binding"}); err != nil {
		t.Fatalf("bind pipeline value: %v", err)
	}
	got, err = evaluate(ctx, request)
	if err != nil {
		t.Fatalf("evaluate bound reference: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]any{"value": "binding"}) {
		t.Fatalf("bound projection output = %#v, want binding value", got)
	}
}
