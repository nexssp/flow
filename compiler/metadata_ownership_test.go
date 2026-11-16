package compiler_test

import (
	"context"
	"maps"
	"testing"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/symbols"
)

type cloneableMetadata struct {
	values map[string]string
}

func (m cloneableMetadata) CloneMeta() any {
	values := maps.Clone(m.values)
	return cloneableMetadata{values: values}
}

type sharedMetadata struct {
	values map[string]string
}

func TestPreparedSourceMetadataReturnsFreshTopLevelSnapshot(t *testing.T) {
	prepared := compiler.PreparedSourceFromPreprocessed(
		"source", "snapshot.nflow", "clean", map[string]any{"kept": "original"},
	)

	first := prepared.Metadata()
	first["kept"] = "changed"
	first["added"] = true

	second := prepared.Metadata()
	if got := second["kept"]; got != "original" {
		t.Fatalf("Metadata()[\"kept\"] after snapshot mutation = %v, want original", got)
	}
	if _, exists := second["added"]; exists {
		t.Fatal("top-level snapshot mutation appeared in a later Metadata call")
	}
}

func TestPreparedSourceMetadataClonesBuiltInSymbols(t *testing.T) {
	prepared, err := compiler.PrepareSource(
		context.Background(),
		compiler.NewDirectiveTable(symbols.Directive),
		"@symbols local {\n  value: \"original\"\n}\n",
		"symbols.nflow",
	)
	if err != nil {
		t.Fatal(err)
	}

	first := prepared.Metadata()
	namespaces, ok := first["symbols"].(map[string]compiler.SymbolNamespace)
	if !ok {
		t.Fatalf("symbols metadata type = %T, want map[string]compiler.SymbolNamespace", first["symbols"])
	}
	namespace, ok := namespaces["local"]
	if !ok {
		t.Fatal("symbols metadata does not contain local namespace")
	}
	namespace.Symbols["value"] = "changed"
	namespace.Symbols["added"] = "caller value"
	namespaces["added"] = compiler.SymbolNamespace{Name: "added", Symbols: map[string]any{"value": "caller value"}}
	first["symbols"] = nil
	first["added"] = true

	second := prepared.Metadata()
	secondNamespaces, ok := second["symbols"].(map[string]compiler.SymbolNamespace)
	if !ok {
		t.Fatalf("symbols metadata after snapshot mutation = %T, want map[string]compiler.SymbolNamespace", second["symbols"])
	}
	secondLocal, ok := secondNamespaces["local"]
	if !ok {
		t.Fatal("local namespace was changed through the metadata snapshot")
	}
	if got := secondLocal.Symbols["value"]; got != "original" {
		t.Fatalf("local symbol after snapshot mutation = %v, want original", got)
	}
	if _, exists := secondLocal.Symbols["added"]; exists {
		t.Fatal("nested symbol-map mutation appeared in a later Metadata call")
	}
	if _, exists := secondNamespaces["added"]; exists {
		t.Fatal("namespace-map mutation appeared in a later Metadata call")
	}
	if _, exists := second["added"]; exists {
		t.Fatal("top-level mutation appeared in a later Metadata call")
	}
}

func TestPreparedSourceMetadataUsesExtensionMetaClone(t *testing.T) {
	prepared := compiler.PreparedSourceFromPreprocessed(
		"source", "extension.nflow", "clean",
		map[string]any{"custom": cloneableMetadata{values: map[string]string{"key": "original"}}},
	)

	first := prepared.Metadata()
	custom, ok := first["custom"].(cloneableMetadata)
	if !ok {
		t.Fatalf("custom metadata type = %T, want cloneableMetadata", first["custom"])
	}
	custom.values["key"] = "changed"

	second := prepared.Metadata()
	custom, ok = second["custom"].(cloneableMetadata)
	if !ok {
		t.Fatalf("custom metadata type after snapshot mutation = %T, want cloneableMetadata", second["custom"])
	}
	if got := custom.values["key"]; got != "original" {
		t.Fatalf("extension metadata after snapshot mutation = %q, want original", got)
	}
}

func TestPreparedSourceMetadataSharesUncloneableExtensionValues(t *testing.T) {
	prepared := compiler.PreparedSourceFromPreprocessed(
		"source", "shared.nflow", "clean",
		map[string]any{"custom": sharedMetadata{values: map[string]string{"key": "original"}}},
	)

	first := prepared.Metadata()
	custom, ok := first["custom"].(sharedMetadata)
	if !ok {
		t.Fatalf("custom metadata type = %T, want sharedMetadata", first["custom"])
	}
	custom.values["key"] = "shared mutation"

	second := prepared.Metadata()
	custom, ok = second["custom"].(sharedMetadata)
	if !ok {
		t.Fatalf("custom metadata type after mutation = %T, want sharedMetadata", second["custom"])
	}
	if got := custom.values["key"]; got != "shared mutation" {
		t.Fatalf("uncloneable extension metadata = %q, want shared mutation", got)
	}
}
