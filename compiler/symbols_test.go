package compiler

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestRegisterAndLookupSymbol(t *testing.T) {
	t.Parallel()
	RegisterNamespace(SymbolNamespace{
		Name: "symtest_basic",
		Symbols: map[string]any{
			"answer": 42,
			"greet":  "hello",
		},
	})

	value, ok := LookupSymbol("symtest_basic", "answer")
	ktest.RequireCondition(t, ok, "answer not found")
	ktest.RequireEqual(t, value, 42)

	_, ok = LookupSymbol("symtest_basic", "missing")
	ktest.RequireCondition(t, !ok, "missing symbol reported as present")

	_, ok = LookupSymbol("symtest_unknown", "answer")
	ktest.RequireCondition(t, !ok, "unknown namespace reported as present")
}

func TestRegisterNamespace_EmptyIsNoop(t *testing.T) {
	t.Parallel()
	RegisterNamespace(SymbolNamespace{Name: ""})
	RegisterNamespace(SymbolNamespace{Name: "symtest_empty"})
	RegisterNamespace(SymbolNamespace{Name: "symtest_empty", Symbols: map[string]any{}})

	_, ok := LookupSymbol("symtest_empty", "anything")
	ktest.RequireCondition(t, !ok, "empty namespace should not be registered")
}

func TestRegisterNamespace_CopiesSymbols(t *testing.T) {
	t.Parallel()
	source := map[string]any{"value": 1}
	RegisterNamespace(SymbolNamespace{Name: "symtest_copy", Symbols: source})

	source["value"] = 99

	value, ok := LookupSymbol("symtest_copy", "value")
	ktest.RequireCondition(t, ok, "value not found after source mutation")
	ktest.RequireEqual(t, value, 1)
}

func TestRegisterNamespace_ReplacesExisting(t *testing.T) {
	t.Parallel()
	RegisterNamespace(SymbolNamespace{Name: "symtest_replace", Symbols: map[string]any{"v": 1}})
	RegisterNamespace(SymbolNamespace{Name: "symtest_replace", Symbols: map[string]any{"v": 2}})

	value, ok := LookupSymbol("symtest_replace", "v")
	ktest.RequireCondition(t, ok, "v not found after re-registration")
	ktest.RequireEqual(t, value, 2)
}

func TestWithSymbolNamespaces_BindsWithoutOverwriting(t *testing.T) {
	t.Parallel()
	RegisterNamespace(SymbolNamespace{
		Name:    "symtest_bind",
		Symbols: map[string]any{"value": "from_registry"},
	})

	existing := map[string]any{"symtest_bind": "from_caller"}
	out := WithSymbolNamespaces(existing)
	ktest.RequireEqual(t, out["symtest_bind"], "from_caller")

	fresh := WithSymbolNamespaces(map[string]any{})
	ns, ok := fresh["symtest_bind"].(map[string]any)
	ktest.RequireCondition(t, ok, "symtest_bind not bound as map: %T", fresh["symtest_bind"])
	ktest.RequireEqual(t, ns["value"], "from_registry")
}

func TestNamespaces_ReturnsSortedSnapshot(t *testing.T) {
	t.Parallel()
	RegisterNamespace(SymbolNamespace{Name: "symtest_zzz", Symbols: map[string]any{"z": 1}})
	RegisterNamespace(SymbolNamespace{Name: "symtest_aaa", Symbols: map[string]any{"a": 1}})

	names := Namespaces()
	var sawA, sawZ bool
	prev := ""
	sorted := true
	for _, ns := range names {
		switch ns.Name {
		case "symtest_aaa":
			sawA = true
		case "symtest_zzz":
			sawZ = true
		}
		if ns.Name < prev {
			sorted = false
		}
		prev = ns.Name
	}
	ktest.RequireCondition(t, sawA && sawZ, "expected both symtest_aaa and symtest_zzz in %v", names)
	ktest.RequireCondition(t, sorted, "namespaces not sorted: %v", names)
}
