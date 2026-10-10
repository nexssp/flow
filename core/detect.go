package core

import "strings"

// DetectRequiredBundles inspects an AST and returns the list of bundle IDs
// that must be compiled into the binary.
func DetectRequiredBundles(expr Expr) []string {
	seen := map[string]bool{
		"syntax":   true, // Binary operators (->, |, &, ||)
		"runtime":  true, // Core literals (const, noop, pick, etc.)
		"pipeline": true, // Pipeline mechanics
	}

	walkExpr(expr, func(node Expr) {
		switch n := node.(type) {
		case *Atom:
			if prefix, _, ok := strings.Cut(n.Name, "."); ok {
				seen[prefix] = true
			}
		case *LoopExpr:
			seen["loop"] = true
		case *AssertExpr:
			seen["assert"] = true
		case *ProjectionExpr:
			seen["projection"] = true
		}
	})

	bundles := make([]string, 0, len(seen))
	for bundle := range seen {
		bundles = append(bundles, bundle)
	}
	return bundles
}
