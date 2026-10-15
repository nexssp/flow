package resilience

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

// Modifiers exposes only the standalone breaker selector. Existing retry,
// timeout, and concurrency modifiers remain Kernel-owned and are consumed by
// the single resilience adviser when this bundle is loaded.
func Modifiers() []compiler.Modifier {
	return []compiler.Modifier{
		compiler.WithInheritable(compiler.WithUnique(compiler.Modifier{
			Name:      "circuit_breaker",
			ValueKind: compiler.ModifierKindAny,
			Example:   ":circuit_breaker=payments",
			Apply: func(*action.Builder[any, any], string) error {
				// The AtomAdvise hook consumes this modifier and installs the
				// validated Kernel middleware before ModifierTable.ApplyAll.
				return nil
			},
		})),
	}
}
