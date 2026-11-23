package projection

import (
	"reflect"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestCheckTypedSourceExpressionWithNilFunctionSet(t *testing.T) {
	var functions *FunctionSet
	ctx := compiler.TypeAnalysisContext{ExpressionCompiler: functions}
	if _, err := CheckTypedSourceExpression("1 + true", ctx, compiler.Position{}); err == nil {
		t.Fatal("expected invalid expression to return an error")
	}
}

func TestTypedProjectionEnvironmentLeavesAnyPipelineInputUndefined(t *testing.T) {
	t.Parallel()
	type payloadWithRequest struct {
		Request map[string]any `json:"request"`
	}
	anyType := reflect.TypeFor[any]()
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[payloadWithRequest]()},
		Bindings: map[string]compiler.TypeDescriptor{
			"request": {Type: anyType},
		},
		PipelineInputBindings: map[string]struct{}{"request": {}},
	}
	env, staticTypes := typedProjectionEnvironment(ctx, 2)
	if _, exists := env["request"]; exists {
		t.Fatal("any typed input was inserted into expr environment as nil or a shadowed payload alias")
	}
	if got := staticTypes["request"]; got != anyType {
		t.Fatalf("static request type = %v, want any", got)
	}
	if _, err := compileTypedExpression("request.current", env, staticTypes, nil, compiler.Position{}); err != nil {
		t.Fatalf("compile any-typed named input path: %v", err)
	}
}
