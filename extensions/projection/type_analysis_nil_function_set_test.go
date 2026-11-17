package projection

import (
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
