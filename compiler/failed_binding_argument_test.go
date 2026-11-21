package compiler

import (
	"strings"
	"testing"
)

func TestResolveArgumentExpressionRejectsUnavailableBinding(t *testing.T) {
	ctx := WithExpressionScope(t.Context(), map[string]any{})
	MarkExpressionBindingUnavailable(ctx, "failed")

	for _, tc := range []struct {
		name       string
		expression string
		wantError  bool
	}{
		{name: "direct reference", expression: "failed", wantError: true},
		{name: "member reference", expression: "failed.value", wantError: true},
		{name: "string literal", expression: `"failed"`},
		{name: "object key", expression: `{"failed": "literal"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program, err := CompileRuntimeExpression(tc.expression, Position{}, nil)
			if err != nil {
				t.Fatalf("CompileRuntimeExpression(%q): %v", tc.expression, err)
			}
			value := &Value{Kind: ValueExpression, Expression: tc.expression, Program: program}
			_, err = resolveValue(ctx, value, nil)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), `pipeline binding "failed" is unavailable because its action failed`) {
					t.Fatalf("resolveValue() error = %v, want unavailable-binding error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveValue() error = %v, want a valid non-reference expression", err)
			}
		})
	}
}
