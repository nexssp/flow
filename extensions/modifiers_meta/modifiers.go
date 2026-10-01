package modifiers_meta

import (
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

func Modifiers() []core.Modifier {
	return []core.Modifier{
		core.String("name", (*action.Builder[any, any]).Name),
		core.String("desc", (*action.Builder[any, any]).Description),
		core.String("description", (*action.Builder[any, any]).Description),
		core.Int("status", (*action.Builder[any, any]).SuccessStatus),

		core.StringList("tag", func(b *action.Builder[any, any], tags []string) *action.Builder[any, any] {
			return b.Tag(tags...)
		}),

		core.String("scope", func(b *action.Builder[any, any], s string) *action.Builder[any, any] {
			switch strings.ToLower(s) {
			case "internal":
				return b.Internal()
			case "system":
				return b.System()
			default:
				return b.Public()
			}
		}),

		core.Flag("read_only", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("read_only")
		}),
		core.Flag("audit", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("audit")
		}),
		core.Flag("debug", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("debug")
		}),
		core.Flag("deprecated", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("deprecated")
		}),

		core.Flag("strict", func(b *action.Builder[any, any]) *action.Builder[any, any] { return b }),
		core.Flag("lenient", func(b *action.Builder[any, any]) *action.Builder[any, any] { return b }),
	}
}
