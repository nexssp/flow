package modifiers_auth

import (
	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

// Modifiers returns the identity guards exposed by this bundle. Each
// delegates to the matching Builder method; none is metadata-only.
func Modifiers() []compiler.Modifier {
	return []compiler.Modifier{
		compiler.Flag("auth", (*action.Builder[any, any]).RequireAuth),
		compiler.StringList("role", func(b *action.Builder[any, any], roles []string) *action.Builder[any, any] {
			for _, role := range roles {
				b = b.RequireRole(role)
			}
			return b
		}),
		compiler.StringList("perm", func(b *action.Builder[any, any], perms []string) *action.Builder[any, any] {
			for _, perm := range perms {
				b = b.RequirePermission(perm)
			}
			return b
		}),
		compiler.StringList("feature", func(b *action.Builder[any, any], features []string) *action.Builder[any, any] {
			for _, feature := range features {
				b = b.RequireFeature(feature)
			}
			return b
		}),
	}
}
