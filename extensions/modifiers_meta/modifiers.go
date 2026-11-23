package modifiers_meta

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

func Modifiers() []compiler.Modifier {
	return []compiler.Modifier{
		compiler.String("name", (*action.Builder[any, any]).Name),
		compiler.String("desc", (*action.Builder[any, any]).Description),
		compiler.String("description", (*action.Builder[any, any]).Description),
		exampleModifier(),
		compiler.Int("status", (*action.Builder[any, any]).SuccessStatus),

		compiler.StringList("tag", func(b *action.Builder[any, any], tags []string) *action.Builder[any, any] {
			return b.Tag(tags...)
		}),

		scopeModifier(),

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

func scopeModifier() compiler.Modifier {
	apply := func(b *action.Builder[any, any], value string) error {
		switch strings.ToLower(value) {
		case "public":
			b.Public()
		case "internal":
			b.Internal()
		case "system":
			b.System()
		default:
			return fmt.Errorf("invalid scope %q (want public, internal, or system)", value)
		}
		return nil
	}
	return compiler.Modifier{
		Name:      "scope",
		ValueKind: compiler.ModifierKindString,
		Apply:     apply,
		ApplyNative: func(b *action.Builder[any, any], value any) error {
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("expected string scope, got %T", value)
			}
			return apply(b, s)
		},
	}
}
