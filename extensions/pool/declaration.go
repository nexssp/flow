package pool

import "github.com/nexssp/flow/core"

// Declaration is the parsed form of a single `@pool NAME [...] { ... }`
// directive.
type Declaration struct {
	Name     string
	Members  []string
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

// PoolsFromMeta reduces the declarations to a name → members map used
// by the dispatch action.
func PoolsFromMeta(meta map[string]any) map[string][]string {
	declarations := DeclarationsFromMeta(meta)
	if len(declarations) == 0 {
		return nil
	}
	out := make(map[string][]string, len(declarations))
	for _, declaration := range declarations {
		out[declaration.Name] = append([]string(nil), declaration.Members...)
	}
	return out
}
