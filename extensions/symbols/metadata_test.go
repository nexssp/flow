package symbols

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestMergeSymbolsMetadataDuplicateDoesNotMutateInputs(t *testing.T) {
	parentNested := map[string]any{"items": []any{"parent"}}
	includedNested := map[string]any{"items": []any{"included"}}
	parentNamespaces := map[string]compiler.SymbolNamespace{
		"routing": {Name: "routing", Symbols: map[string]any{"nested": parentNested}},
	}
	includedNamespaces := map[string]compiler.SymbolNamespace{
		"routing": {Name: "routing", Symbols: map[string]any{"nested": includedNested}},
	}
	parent := map[string]any{"symbols": parentNamespaces}
	included := map[string]any{"symbols": includedNamespaces}

	if err := mergeSymbolsMetadata(parent, included); err == nil {
		t.Fatal("expected duplicate namespace error")
	}
	parentMerged, ok := parent["symbols"].(map[string]compiler.SymbolNamespace)
	ktest.RequireCondition(t, ok, "parent symbols metadata type: got %T", parent["symbols"])
	parentNestedValue, ok := parentMerged["routing"].Symbols["nested"].(map[string]any)
	ktest.RequireCondition(t, ok, "parent nested symbol type: got %T", parentMerged["routing"].Symbols["nested"])
	parentItems, ok := parentNestedValue["items"].([]any)
	ktest.RequireCondition(t, ok, "parent nested items type: got %T", parentNestedValue["items"])
	ktest.RequireEqual(t, parentItems[0], "parent")
	includedMerged, ok := included["symbols"].(map[string]compiler.SymbolNamespace)
	ktest.RequireCondition(t, ok, "included symbols metadata type: got %T", included["symbols"])
	includedNestedValue, ok := includedMerged["routing"].Symbols["nested"].(map[string]any)
	ktest.RequireCondition(t, ok, "included nested symbol type: got %T", includedMerged["routing"].Symbols["nested"])
	includedItems, ok := includedNestedValue["items"].([]any)
	ktest.RequireCondition(t, ok, "included nested items type: got %T", includedNestedValue["items"])
	ktest.RequireEqual(t, includedItems[0], "included")
}

func TestMergeSymbolsMetadataDeepCopiesNestedValues(t *testing.T) {
	nested := map[string]any{"items": []any{"one"}}
	parentNamespaces := map[string]compiler.SymbolNamespace{
		"parent": {Name: "parent", Symbols: map[string]any{"nested": nested}},
	}
	includedNamespaces := map[string]compiler.SymbolNamespace{
		"included": {Name: "included", Symbols: map[string]any{"nested": nested}},
	}
	parent := map[string]any{"symbols": parentNamespaces}
	included := map[string]any{"symbols": includedNamespaces}

	if err := mergeSymbolsMetadata(parent, included); err != nil {
		t.Fatal(err)
	}
	merged, ok := parent["symbols"].(map[string]compiler.SymbolNamespace)
	ktest.RequireCondition(t, ok, "merged symbols metadata type: got %T", parent["symbols"])
	mergedNested, ok := merged["included"].Symbols["nested"].(map[string]any)
	ktest.RequireCondition(t, ok, "merged nested symbol type: got %T", merged["included"].Symbols["nested"])
	mergedItems, ok := mergedNested["items"].([]any)
	ktest.RequireCondition(t, ok, "merged nested items type: got %T", mergedNested["items"])
	mergedItems[0] = "changed"
	nestedItems, ok := nested["items"].([]any)
	ktest.RequireCondition(t, ok, "input nested items type: got %T", nested["items"])
	ktest.RequireCondition(t, nestedItems[0] == "one", "merged symbols retained aliases to input maps/slices")
}
