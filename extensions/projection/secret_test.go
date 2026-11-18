package projection

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/secret"
)

type secretExpressionInput struct {
	APIKey secret.Value `json:"api_key"`
}

func TestTypedSourceExpressionAllowsOnlySecretPassThrough(t *testing.T) {
	ctx := compiler.TypeAnalysisContext{Input: compiler.TypeDescriptor{Type: reflect.TypeFor[secretExpressionInput]()}}
	passThrough, err := CheckTypedSourceExpression(".api_key", ctx, compiler.Position{})
	if err != nil {
		t.Fatalf("direct Secret pass-through was rejected: %v", err)
	}
	if passThrough.Type != reflect.TypeFor[secret.Value]() {
		t.Fatalf("pass-through type = %v, want secret.Value", passThrough.Type)
	}

	for _, source := range []string{
		`.api_key + "suffix"`,
		`string(.api_key)`,
		`sprintf("%s", .api_key)`,
	} {
		if _, err := CheckTypedSourceExpression(source, ctx, compiler.Position{}); err == nil || !strings.Contains(err.Error(), "secret values cannot be") {
			t.Errorf("expression %q error = %v, want Secret-operation rejection", source, err)
		}
	}
}
