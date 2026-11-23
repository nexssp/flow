package projection

import (
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestProjectionActionInjectedRootPreservesInput(t *testing.T) {
	const raw = "name: .name"
	root := map[string]any{"name": "Julia"}
	input := map[string]any{"raw": raw, expressionRootName: root}
	project := projectionAction(NewExprEvaluator())

	got, err := action.InvokeAny(t.Context(), project, input)
	if err != nil {
		t.Fatalf("invoke projection: %v", err)
	}
	want := map[string]any{"name": "Julia"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection result = %#v, want %#v", got, want)
	}
	if input["raw"] != raw || !reflect.DeepEqual(input[expressionRootName], root) || len(input) != 2 {
		t.Fatalf("projection mutated action input: %#v", input)
	}
}

func TestProjectionActionCopiesWhenRootHasOtherStateFields(t *testing.T) {
	input := map[string]any{
		"raw":              "value: .extra",
		expressionRootName: map[string]any{"name": "Julia"},
		"extra":            "kept",
	}
	project := projectionAction(NewExprEvaluator())

	got, err := action.InvokeAny(t.Context(), project, input)
	if err != nil {
		t.Fatalf("invoke projection: %v", err)
	}
	want := map[string]any{"value": "kept"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection result = %#v, want %#v", got, want)
	}
	if input["raw"] != "value: .extra" || input["extra"] != "kept" || len(input) != 3 {
		t.Fatalf("projection mutated action input: %#v", input)
	}
}
