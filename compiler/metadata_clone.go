package compiler

import (
	"maps"
	"slices"
)

// MetaClone lets a metadata value opt into independent snapshots returned by
// PreparedSource.Metadata. CloneMeta must return an equivalent value whose
// mutable state does not alias the receiver; it is responsible for cloning its
// own nested mutable values. Pointer-receiver implementations must also handle a
// typed nil receiver if such a value can be stored in metadata.
type MetaClone interface {
	CloneMeta() any
}

func cloneMetadataSnapshot(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}

	snapshot := make(map[string]any, len(meta))
	for key, value := range meta {
		snapshot[key] = cloneMetadataValue(value)
	}
	return snapshot
}

func cloneMetadataValue(value any) any {
	if value == nil {
		return nil
	}
	if cloneable, ok := value.(MetaClone); ok {
		return cloneable.CloneMeta()
	}

	switch value := value.(type) {
	case *sourceScope:
		return cloneSourceScope(value)
	case []SourceLine:
		return cloneSourceLines(value)
	case map[string]SymbolNamespace:
		return cloneSymbolNamespaces(value)
	case TypedSymbolValue:
		value.Value = cloneMetadataValue(value.Value)
		return value
	case *TypedSymbolValue:
		if value == nil {
			return (*TypedSymbolValue)(nil)
		}
		cloned := *value
		cloned.Value = cloneMetadataValue(cloned.Value)
		return &cloned
	default:
		return value
	}
}

func cloneSymbolNamespaces(namespaces map[string]SymbolNamespace) map[string]SymbolNamespace {
	if namespaces == nil {
		return nil
	}

	cloned := make(map[string]SymbolNamespace, len(namespaces))
	for name, namespace := range namespaces {
		if namespace.Symbols != nil {
			symbols := make(map[string]any, len(namespace.Symbols))
			for symbol, value := range namespace.Symbols {
				symbols[symbol] = cloneMetadataValue(value)
			}
			namespace.Symbols = symbols
		}
		cloned[name] = namespace
	}
	return cloned
}

func cloneSourceLines(lines []SourceLine) []SourceLine {
	if lines == nil {
		return nil
	}

	cloned := slices.Clone(lines)
	for i := range cloned {
		cloned[i].Origins = slices.Clone(lines[i].Origins)
	}
	return cloned
}

func cloneSourceScope(scope *sourceScope) *sourceScope {
	if scope == nil {
		return nil
	}

	cloned := *scope
	cloned.declarations = maps.Clone(scope.declarations)
	cloned.imports = slices.Clone(scope.imports)
	cloned.index = cloneSourceIndex(scope.index)
	return &cloned
}

func cloneSourceIndex(index SourceIndex) SourceIndex {
	index.Declarations = slices.Clone(index.Declarations)
	index.References = slices.Clone(index.References)
	index.Blocks = slices.Clone(index.Blocks)
	index.Imports = slices.Clone(index.Imports)
	index.Tokens = slices.Clone(index.Tokens)
	return index
}
