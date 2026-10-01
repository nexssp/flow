// Package schema provides the `@schema NAME { Field Type `json:"..."
// validate:"..."` ... }` directive. The declaration lands in
// meta["schemas"]; the `:schema=NAME` modifier attaches the name to an
// atom so the strict-mode validation hook can find it.
//
// Typical use:
//
//	@schema TestUser {
//	  Name  string `json:"name"  validate:"required"`
//	  Email string `json:"email" validate:"email"`
//	}
//	{ name: "Ada" } -> noop:schema=TestUser
package schema

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "schema"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{Directive},
		Modifiers:  []core.Modifier{SchemaModifier},
		Fixtures:   fixturesFS,
	}
}

// SchemaModifier attaches a schema name to an atom as a tag. The tag
// is read by the strict-mode validation hook.
var SchemaModifier = core.String("schema",
	func(b *action.Builder[any, any], name string) *action.Builder[any, any] {
		return b.Tag("schema:" + name)
	})
