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
// so that programs in any repo use them without local declarations. Resolved
// values bind during build; unresolved bare values remain ordinary literals in
// data positions, while expression validity depends on the expression's env.
type SymbolNamespace struct {
	Name    string
	Symbols map[string]any
}

// TypedSymbolValue carries an explicitly annotated @symbols value through
// compile-time metadata without erasing its declared type. Value remains in
// its native Go representation.
type TypedSymbolValue struct {
	Type  string
	Value any
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

// LookupSymbol looks up `<namespace>.<name>` in the global registry. The bool
// result lets callers distinguish a missing symbol from a registered nil value.
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
		out[name] = RuntimeSymbolValue(ns.Symbols)
	}
	return out
}

var localSymbolsKey = xctx.NewKey[map[string]SymbolNamespace]("flow.local_symbols")

// WithLocalSymbols installs file-local symbol namespaces for one
// compilation. Program source reaches this through the compiler after
// preprocessing; the process-global registry is never touched by source.
func WithLocalSymbols(ctx context.Context, syms map[string]SymbolNamespace) context.Context {
	return localSymbolsKey.With(ctx, syms)
}

// LocalSymbolsFrom returns the file-local namespaces installed on ctx.
func LocalSymbolsFrom(ctx context.Context) map[string]SymbolNamespace {
	syms, _ := localSymbolsKey.From(ctx)
	return syms
}

// SymbolsFromMeta returns the file-local symbol namespaces carried by compile
// metadata. A missing or malformed value is treated as no local symbols.
func SymbolsFromMeta(meta map[string]any) map[string]SymbolNamespace {
	local, _ := meta["symbols"].(map[string]SymbolNamespace)
	imported := SourceImports(meta)
	if len(imported) == 0 {
		return local
	}
	symbols := make(map[string]SymbolNamespace, len(local)+len(imported))
	maps.Copy(symbols, local)
	for _, module := range imported {
		members := make(map[string]any)
		for name, ns := range SymbolsFromMeta(module.Source.Metadata()) {
			members[name] = ns.Symbols
		}
		symbols[module.Namespace] = SymbolNamespace{Name: module.Namespace, Symbols: members}
	}
	return symbols
}

// ResolveSymbolValue resolves a symbol reference and returns its native runtime
// value. Explicit type metadata is unwrapped for compatibility with existing
// consumers.
func ResolveSymbolValue(meta map[string]any, reference string) (any, bool) {
	value, ok := ResolveSymbolDeclaration(meta, reference)
	if !ok {
		return nil, false
	}
	return RuntimeSymbolValue(value), true
}

// ResolveSymbolDeclaration preserves an explicit TypedSymbolValue wrapper for
// compile-time use sites that must check the declared type.
func ResolveSymbolDeclaration(meta map[string]any, reference string) (any, bool) {
	namespace, member, ok := strings.Cut(reference, ".")
	if !ok || namespace == "" || member == "" {
		return nil, false
	}
	if local, exists := SymbolsFromMeta(meta)[namespace]; exists {
		return lookupPath(local.Symbols, member)
	}
	return LookupSymbol(namespace, member)
}

// RuntimeSymbolValue unwraps typed compile-time values for Flow expressions and
// action payloads. Composite values are copied recursively.
func RuntimeSymbolValue(value any) any {
	switch value := value.(type) {
	case TypedSymbolValue:
		return RuntimeSymbolValue(value.Value)
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = RuntimeSymbolValue(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = RuntimeSymbolValue(item)
		}
		return out
	default:
		return value
	}
}

// MergeSymbolNamespaces returns a fresh environment containing every
// registered global namespace, then any file-local namespace from ctx
// (overriding globals of the same name), then the caller's env
// (overriding both). Caller wins on collision, matching
// WithSymbolNamespaces.
func MergeSymbolNamespaces(ctx context.Context, env map[string]any) map[string]any {
	out := WithSymbolNamespaces(nil)
	for name, ns := range LocalSymbolsFrom(ctx) {
		out[name] = RuntimeSymbolValue(ns.Symbols)
	}
	maps.Copy(out, env)
	return out
}
