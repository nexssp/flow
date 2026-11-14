package symbols

import (
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestInheritSymbolScopeCopiesNamespaces(t *testing.T) {
	parentNamespace := compiler.SymbolNamespace{
		Name:    "thresholds",
		Symbols: map[string]any{"high": 90},
	}
	parentMeta := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{"thresholds": parentNamespace},
	}
	childMeta := make(map[string]any)
	if err := inheritSymbolScope(parentMeta, childMeta); err != nil {
		t.Fatalf("inherit symbols: %v", err)
	}

	childSymbols, ok := childMeta["symbols"].(map[string]compiler.SymbolNamespace)
	if !ok {
		t.Fatalf("child symbols = %T, want map[string]compiler.SymbolNamespace", childMeta["symbols"])
	}
	if got := childSymbols["thresholds"].Symbols["high"]; got != 90 {
		t.Fatalf("inherited symbol = %#v, want 90", got)
	}
	childNamespace := childSymbols["thresholds"]
	childNamespace.Symbols["low"] = 50
	if _, exists := parentNamespace.Symbols["low"]; exists {
		t.Fatal("child namespace mutation changed the parent symbol map")
	}
}
