package compiler

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_SymbolsRegisterViaBundleConfig(t *testing.T) {
	RegisterNamespace(SymbolNamespace{Name: "bstest_erase", Symbols: map[string]any{"x": "stale"}})

	BundleConfig(Bundle{
		ID: "bstest_bundle",
		Symbols: []SymbolNamespace{{
			Name:    "bstest_ns",
			Symbols: map[string]any{"answer": 42},
		}},
	})

	value, ok := LookupSymbol("bstest_ns", "answer")
	ktest.RequireCondition(t, ok, "bundle symbol was not registered")
	ktest.RequireEqual(t, value, 42)
}

func TestValidateBundle_RejectsEmptyNamespaceName(t *testing.T) {
	err := ValidateBundle(Bundle{
		ID:      "x",
		Symbols: []SymbolNamespace{{Name: "", Symbols: map[string]any{"a": 1}}},
	})
	ktest.RequireErrorContains(t, err, "empty name")
}

func TestValidateBundle_RejectsEmptyNamespaceSymbols(t *testing.T) {
	err := ValidateBundle(Bundle{
		ID:      "x",
		Symbols: []SymbolNamespace{{Name: "ns"}},
	})
	ktest.RequireErrorContains(t, err, "has no symbols")
}

func TestValidateBundle_RejectsDuplicateNamespace(t *testing.T) {
	err := ValidateBundle(Bundle{
		ID: "x",
		Symbols: []SymbolNamespace{
			{Name: "ns", Symbols: map[string]any{"a": 1}},
			{Name: "ns", Symbols: map[string]any{"b": 2}},
		},
	})
	ktest.RequireErrorContains(t, err, "duplicate symbol namespace")
}
