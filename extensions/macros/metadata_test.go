package macros

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMetadataMergerRejectsDuplicateMacroName(t *testing.T) {
	t.Parallel()
	parent := map[string]any{DeclarationKey: []Declaration{{Name: "greet"}}}
	included := map[string]any{DeclarationKey: []Declaration{{Name: "greet"}}}
	ktest.RequireErrorContains(t, Directive.Metadata.Merge(parent, included), "duplicate declaration")
	declarations, ok := parent[DeclarationKey].([]Declaration)
	ktest.RequireCondition(t, ok, "macro metadata type: got %T", parent[DeclarationKey])
	ktest.RequireEqual(t, len(declarations), 1)
}

func TestMetadataMergerAppendsMacros(t *testing.T) {
	t.Parallel()
	parent := map[string]any{DeclarationKey: []Declaration{{Name: "greet", Params: []string{"name"}}}}
	included := map[string]any{DeclarationKey: []Declaration{{Name: "bye", Body: "noop"}}}
	ktest.RequireNoError(t, Directive.Metadata.Merge(parent, included))
	merged, ok := parent[DeclarationKey].([]Declaration)
	ktest.RequireCondition(t, ok, "merged macro metadata type: got %T", parent[DeclarationKey])
	ktest.RequireEqual(t, len(merged), 2)
	ktest.RequireEqual(t, merged[0].Params, []string{"name"})
	includedDeclarations, ok := included[DeclarationKey].([]Declaration)
	ktest.RequireCondition(t, ok, "included macro metadata type: got %T", included[DeclarationKey])
	ktest.RequireEqual(t, includedDeclarations[0].Name, "bye")
}
