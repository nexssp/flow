package symbols

import (
	"context"
	"embed"
	"fmt"
	"maps"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

//go:embed nflows
var fixturesFS embed.FS

const ID = "symbols"

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:           ID,
		Libraries:    []action.Library{{Name: ID}},
		Directives:   []compiler.Directive{Directive},
		InheritScope: inheritSymbolScope,
		Fixtures:     fixturesFS,
		WrapPipeline: func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
			syms := compiler.SymbolsFromMeta(meta)
			if len(syms) == 0 {
				return inner, nil
			}
			return action.New("symbols.wrap", func(ctx context.Context, req any) (any, error) {
				return action.InvokeAny(compiler.WithLocalSymbols(ctx, syms), inner, req)
			}).Build(), nil
		},
	}
}

func inheritSymbolScope(parentMeta, childMeta map[string]any) error {
	parent := compiler.SymbolsFromMeta(parentMeta)
	if len(parent) == 0 {
		return nil
	}
	child, _ := childMeta["symbols"].(map[string]compiler.SymbolNamespace)
	merged := make(map[string]compiler.SymbolNamespace, len(parent)+len(child))
	copyNamespace := func(namespace compiler.SymbolNamespace) compiler.SymbolNamespace {
		symbols := make(map[string]any, len(namespace.Symbols))
		maps.Copy(symbols, namespace.Symbols)
		namespace.Symbols = symbols
		return namespace
	}
	for name, namespace := range parent {
		namespace.Name = name
		merged[name] = copyNamespace(namespace)
	}
	for name, namespace := range child {
		if _, exists := merged[name]; exists {
			return fmt.Errorf("symbols: namespace %q is already declared in an inherited scope", name)
		}
		namespace.Name = name
		merged[name] = copyNamespace(namespace)
	}
	childMeta["symbols"] = merged
	return nil
}
