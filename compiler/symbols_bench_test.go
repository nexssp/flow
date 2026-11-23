package compiler

import (
	"fmt"
	"testing"
)

var (
	benchmarkNamespaces     []SymbolNamespace
	benchmarkNamespaceNames []string
)

func BenchmarkNamespaceListing(b *testing.B) {
	const namespaceCount = 32
	namespaceNames := make([]string, 0, namespaceCount)
	for namespaceIndex := range namespaceCount {
		symbols := make(map[string]any, 12)
		for symbolIndex := range 12 {
			symbols[fmt.Sprintf("symbol_%02d", symbolIndex)] = symbolIndex
		}
		name := fmt.Sprintf("benchmark_namespace_%02d", namespaceIndex)
		namespaceNames = append(namespaceNames, name)
		RegisterNamespace(SymbolNamespace{
			Name:    name,
			Symbols: symbols,
		})
	}
	b.Cleanup(func() {
		symbolRegistryMu.Lock()
		defer symbolRegistryMu.Unlock()
		for _, name := range namespaceNames {
			delete(symbolRegistry, name)
		}
	})

	b.Run("all-symbols", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchmarkNamespaces = Namespaces()
		}
	})
	b.Run("names-only", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchmarkNamespaceNames = NamespaceNames()
		}
	})
}
