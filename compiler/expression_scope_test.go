package compiler_test

import (
	"reflect"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestExpressionBindingNamesIncludesVisibleParentAndChildBindings(t *testing.T) {
	parent := compiler.WithExpressionScope(t.Context(), nil)
	if err := compiler.BindExpressionValue(parent, "parent_value", "not exposed"); err != nil {
		t.Fatalf("bind parent value: %v", err)
	}
	if err := compiler.BindExpressionValue(parent, "shadowed", "parent"); err != nil {
		t.Fatalf("bind parent shadowed value: %v", err)
	}
	child := compiler.WithChildExpressionScope(parent)
	if err := compiler.BindExpressionValue(child, "child_value", "not exposed"); err != nil {
		t.Fatalf("bind child value: %v", err)
	}
	if err := compiler.BindExpressionValue(child, "shadowed", "child"); err != nil {
		t.Fatalf("bind child shadowed value: %v", err)
	}

	got := compiler.ExpressionBindingNames(child)
	want := []string{"child_value", "parent_value", "shadowed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpressionBindingNames() = %#v, want %#v", got, want)
	}
}

func TestExpressionPipelineInputBindingNamesRespectShadowing(t *testing.T) {
	parent := compiler.WithExpressionScope(t.Context(), nil)
	if err := compiler.BindPipelineInputValue(parent, "request", map[string]any{"value": "typed"}); err != nil {
		t.Fatalf("bind typed pipeline input: %v", err)
	}
	if got := compiler.ExpressionPipelineInputBindingNames(parent); !reflect.DeepEqual(got, map[string]struct{}{"request": {}}) {
		t.Fatalf("typed pipeline input names = %#v, want request", got)
	}

	child := compiler.WithChildExpressionScope(parent)
	if got := compiler.ExpressionPipelineInputBindingNames(child); !reflect.DeepEqual(got, map[string]struct{}{"request": {}}) {
		t.Fatalf("inherited typed pipeline input names = %#v, want request", got)
	}
	if err := compiler.BindExpressionValue(child, "request", map[string]any{"value": "as-output"}); err != nil {
		t.Fatalf("bind shadowing as output: %v", err)
	}
	if got := compiler.ExpressionPipelineInputBindingNames(child); len(got) != 0 {
		t.Fatalf("typed pipeline input names after ordinary binding shadows it = %#v, want empty", got)
	}
}

func TestValidateExpressionBindingNameRejectsParserLiterals(t *testing.T) {
	for _, name := range []string{"true", "false", "nil", "null"} {
		t.Run(name, func(t *testing.T) {
			if err := compiler.ValidateExpressionBindingName(name); err == nil {
				t.Fatalf("ValidateExpressionBindingName(%q) succeeded; want reserved-name error", name)
			}
		})
	}
}
