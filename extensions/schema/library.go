// Package schema provides the `@schema NAME { Field Type `json:"..."
// default:"..." validate:"..."` ... }` directive. The declaration lands in
// meta["schemas"]; the `:schema=NAME` modifier attaches the name to an
// atom so the strict-mode validation hook can find it.
//
// Typical use:
//
//	@schema TestUser {
//	  Name  string `json:"name"  validate:"required"`
//	  Email string `json:"email" validate:"email"`
//	}
//	{ name: "Maksymilian" } -> runtime.noop:schema=TestUser
//
// Missing fields can receive type-aware defaults. ApplyDefaults returns a new
// effective payload map, and Schema.validate passes that map downstream.
package schema

import (
	"context"
	"embed"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "schema"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// rootInputKey carries the value the pipeline received at its outermost
// boundary. schema.validate reads it when the caller did not supply an
// explicit payload, so validation runs against what the pipeline
// actually saw rather than against an empty map.
var rootInputKey = xctx.NewKey[any]("flow.schema.root_input")

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:           ID,
		Libraries:    []action.Library{Library()},
		Directives:   []compiler.Directive{Directive},
		Modifiers:    []compiler.Modifier{SchemaModifier},
		OnPreprocess: wireSchemaAdvisor,
		Materialize:  materialize,
		WrapPipeline: func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
			schemas := SchemasFromMap(meta)
			return action.New("schema.wrap", func(ctx context.Context, req any) (any, error) {
				execCtx := WithContextSchemas(ctx, schemas)
				execCtx = rootInputKey.With(execCtx, req)
				return action.InvokeAny(execCtx, inner, req)
			}).Build(), nil
		},
		Fixtures: fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			ValidateAction,
		},
	}
}

// SchemaModifier attaches a schema name to an atom as a tag.
var SchemaModifier = compiler.String("schema",
	func(b *action.Builder[any, any], name string) *action.Builder[any, any] {
		return b.Tag("schema:" + name)
	})
