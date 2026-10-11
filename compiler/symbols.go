package compiler

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/nexssp/kernel/xctx"
)

// SymbolNamespace groups compile-time symbolic values that .nflow programs
// reference as `<namespace>.<name>`. Bundles register namespaces from init
// so that programs in any repo use them without local declarations. Symbols
// resolve during parse/analyze (unknown names fail compilation) and bind
// into the expression environment at build time.
type SymbolNamespace struct {
	Name    string
	Symbols map[string]any
}

var (
	symbolRegistryMu sync.RWMutex
	symbolRegistry   = make(map[string]SymbolNamespace)
)

// RegisterNamespace adds or replaces a symbolic namespace. Bundles call it
// from init(). Re-registration under the same name replaces the previous
// definition, so tests and hot-reload do not need explicit cleanup. The
// Symbols map is copied; the caller retains ownership of the original.
func RegisterNamespace(ns SymbolNamespace) {
	if ns.Name == "" || len(ns.Symbols) == 0 {
		return
	}
	cp := make(map[string]any, len(ns.Symbols))
	maps.Copy(cp, ns.Symbols)
	symbolRegistryMu.Lock()
	symbolRegistry[ns.Name] = SymbolNamespace{Name: ns.Name, Symbols: cp}
	symbolRegistryMu.Unlock()
}

// LookupSymbol resolves `<namespace>.<name>`. Analysis uses it to reject
// unknown symbols before a program runs.
func LookupSymbol(namespace, name string) (any, bool) {
	symbolRegistryMu.RLock()
	defer symbolRegistryMu.RUnlock()
	ns, ok := symbolRegistry[namespace]
	if !ok {
		return nil, false
	}
	value, ok := ns.Symbols[name]
	return value, ok
}

// Namespaces returns a copy of every registered namespace, sorted by name.
// Callers cannot mutate the registry through the result.
func Namespaces() []SymbolNamespace {
	symbolRegistryMu.RLock()
	defer symbolRegistryMu.RUnlock()
	out := make([]SymbolNamespace, 0, len(symbolRegistry))
	for _, ns := range symbolRegistry {
		cp := make(map[string]any, len(ns.Symbols))
		maps.Copy(cp, ns.Symbols)
		out = append(out, SymbolNamespace{Name: ns.Name, Symbols: cp})
	}
	slices.SortFunc(out, func(a, b SymbolNamespace) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// WithSymbolNamespaces returns a fresh environment with every registered
// namespace bound under its name. Caller-supplied keys win on collision.
// Namespace maps are shared read-only; do not mutate them.
func WithSymbolNamespaces(env map[string]any) map[string]any {
	symbolRegistryMu.RLock()
	defer symbolRegistryMu.RUnlock()

	out := make(map[string]any, len(env)+len(symbolRegistry))
	maps.Copy(out, env)
	for name, ns := range symbolRegistry {
		if _, exists := out[name]; exists {
			continue
		}
		out[name] = ns.Symbols
	}
	return out
}

var localSymbolsKey = xctx.NewKey[map[string]SymbolNamespace]("flow.local_symbols")

// WithLocalSymbols installs file-local symbol namespaces for one
// compilation. Program source reaches this through the compiler after
// preprocessing; the process-global registry is never touched by source.
func WithLocalSymbols(ctx context.Context, syms map[string]SymbolNamespace) context.Context {
	if len(syms) == 0 {
		return ctx
	}
	return localSymbolsKey.With(ctx, syms)
}

// LocalSymbolsFrom returns the file-local namespaces installed on ctx.
func LocalSymbolsFrom(ctx context.Context) map[string]SymbolNamespace {
	syms, _ := localSymbolsKey.From(ctx)
	return syms
}

// MergeSymbolNamespaces returns a fresh environment containing every
// registered global namespace, then any file-local namespace from ctx
// (overriding globals of the same name), then the caller's env
// (overriding both). Caller wins on collision, matching
// WithSymbolNamespaces.
func MergeSymbolNamespaces(ctx context.Context, env map[string]any) map[string]any {
	out := WithSymbolNamespaces(nil)
	for name, ns := range LocalSymbolsFrom(ctx) {
		out[name] = ns.Symbols
	}
	maps.Copy(out, env)
	return out
}
