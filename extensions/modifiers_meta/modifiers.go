package modifiers_meta

import (
	"strings"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

func Modifiers() []compiler.Modifier {
	return []compiler.Modifier{
		compiler.String("name", (*action.Builder[any, any]).Name),
		compiler.String("desc", (*action.Builder[any, any]).Description),
		compiler.String("description", (*action.Builder[any, any]).Description),
		compiler.Int("status", (*action.Builder[any, any]).SuccessStatus),

		compiler.StringList("tag", func(b *action.Builder[any, any], tags []string) *action.Builder[any, any] {
			return b.Tag(tags...)
		}),

		compiler.String("scope", func(b *action.Builder[any, any], s string) *action.Builder[any, any] {
			switch strings.ToLower(s) {
			case "internal":
				return b.Internal()
			case "system":
				return b.System()
			default:
				return b.Public()
			}
		}),

		compiler.Flag("read_only", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("read_only")
		}),
		compiler.Flag("audit", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("audit")
		}),
		compiler.Flag("debug", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("debug")
		}),
		compiler.Flag("deprecated", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Tag("deprecated")
		}),

		compiler.Flag("strict", func(b *action.Builder[any, any]) *action.Builder[any, any] { return b }),
		compiler.Flag("lenient", func(b *action.Builder[any, any]) *action.Builder[any, any] { return b }),
	}
}
