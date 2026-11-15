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
