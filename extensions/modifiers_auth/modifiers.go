package modifiers_auth

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

// Modifiers returns the identity guards exposed by this bundle. Each
// delegates to the matching Builder method; none is metadata-only.
func Modifiers() []core.Modifier {
	return []core.Modifier{
		core.Flag("auth", (*action.Builder[any, any]).RequireAuth),
		core.StringList("role", func(b *action.Builder[any, any], roles []string) *action.Builder[any, any] {
			for _, role := range roles {
				b = b.RequireRole(role)
			}
			return b
		}),
		core.StringList("perm", func(b *action.Builder[any, any], perms []string) *action.Builder[any, any] {
			for _, perm := range perms {
				b = b.RequirePermission(perm)
			}
			return b
		}),
		core.StringList("feature", func(b *action.Builder[any, any], features []string) *action.Builder[any, any] {
			for _, feature := range features {
				b = b.RequireFeature(feature)
			}
			return b
		}),
	}
}
