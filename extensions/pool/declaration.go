package pool

import "github.com/nexssp/flow/core"

// Declaration is the parsed form of a single `@pool NAME [...] { ... }`
// directive. Members are statically resolved capability references;
// canonical names are checked when the pool is materialized.
type Declaration struct {
	Name     string
	Members  []core.CapabilityRef
	Strategy string
	Options  map[string]string
	Position core.Position
}

// DeclarationsFromMeta returns the list of @pool declarations in the
// order they appeared. Nil-safe.
func DeclarationsFromMeta(meta map[string]any) []Declaration {
	declarations, _ := meta["pools"].([]Declaration)
	return declarations
}

// PoolsFromMeta reduces the declarations to a canonical name → members
// map used by the dispatch action.
func PoolsFromMeta(meta map[string]any) map[string][]string {
	declarations := DeclarationsFromMeta(meta)
	if len(declarations) == 0 {
		return nil
	}
	out := make(map[string][]string, len(declarations))
	for _, d := range declarations {
		names := make([]string, len(d.Members))
		for i, m := range d.Members {
			names[i] = m.Canonical
		}
		out[d.Name] = names
	}
	return out
}
