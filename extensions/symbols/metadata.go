package symbols

import (
	"fmt"
	"reflect"

	"github.com/nexssp/flow/compiler"
)

func symbolsMetadata() *compiler.MetadataMerger {
	return &compiler.MetadataMerger{
		Keys:  []string{"symbols"},
		Merge: mergeSymbolsMetadata,
	}
}

func mergeSymbolsMetadata(parent, included map[string]any) error {
	incoming, _ := included["symbols"].(map[string]compiler.SymbolNamespace)
	parentSymbols, _ := parent["symbols"].(map[string]compiler.SymbolNamespace)
	merged := cloneNamespaces(parentSymbols)
	for name, namespace := range incoming {
		if _, duplicate := merged[name]; duplicate {
			return fmt.Errorf("duplicate @symbols namespace %q", name)
		}
		merged[name] = cloneNamespace(namespace)
	}
	parent["symbols"] = merged
	return nil
}

func cloneNamespaces(source map[string]compiler.SymbolNamespace) map[string]compiler.SymbolNamespace {
	clone := make(map[string]compiler.SymbolNamespace, len(source))
	for name, namespace := range source {
		clone[name] = cloneNamespace(namespace)
	}
	return clone
}

func cloneNamespace(namespace compiler.SymbolNamespace) compiler.SymbolNamespace {
	return compiler.SymbolNamespace{Name: namespace.Name, Symbols: cloneAnyMap(namespace.Symbols)}
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		cloned := cloneMetadataValue(reflect.ValueOf(value))
		if !cloned.IsValid() {
			clone[key] = nil
			continue
		}
		clone[key] = cloned.Interface()
	}
	return clone
}

// cloneMetadataValue recursively copies maps and slices while retaining their
// concrete types. Symbol values are normally scalars, but callers may supply
// structured values and must not retain mutable aliases through a merge.
func cloneMetadataValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	kind := value.Kind()
	if kind == reflect.Interface {
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneMetadataValue(value.Elem())
		out := reflect.New(value.Type()).Elem()
		out.Set(cloned)
		return out
	}
	if kind == reflect.Map {
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			out.SetMapIndex(cloneMetadataValue(iter.Key()), cloneMetadataValue(iter.Value()))
		}
		return out
	}
	if kind == reflect.Slice {
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := range value.Len() {
			out.Index(i).Set(cloneMetadataValue(value.Index(i)))
		}
		return out
	}
	if kind == reflect.Array {
		out := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			out.Index(i).Set(cloneMetadataValue(value.Index(i)))
		}
		return out
	}
	return value
}
