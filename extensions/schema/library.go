// Package schema provides the `@schema NAME { Field Type `json:"..."
// default:"..." validate:"..."` ... }` directive. The declaration lands in
// meta["schemas"]. `:request-schema=NAME` and `:response-schema=NAME` attach
// explicit pipeline boundary contracts; prefixing the name with `!` rejects
// undeclared fields at that boundary. Requests receive defaults before
// validation; response schemas are validated without defaults and must not
// declare any `default` tags. Such declarations fail before execution. Extra
// fields remain allowed unless the corresponding boundary is strict.
//
// Typical use:
//
//	@schema TestUser {
//	  Name string `json:"name" default:"guest" validate:"required"`
//	}
//	@pipeline greet
//	  :request-schema=TestUser
//	  runtime.noop
//	@end
//
// ApplyDefaults returns a new effective payload map, and schema validation
// passes that map to the pipeline's first action.
// NormalizeObject exposes encoding/json-compatible object normalization for Go
// values. Response boundaries validate this normalized view but return the
// original result, preserving typed values for callers and later composition.
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
		Modifiers:    []compiler.Modifier{SchemaModifier, RequestSchemaModifier, ResponseSchemaModifier},
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

const (
	SchemaModifierName         = "schema"
	RequestSchemaModifierName  = "request-schema"
	ResponseSchemaModifierName = "response-schema"
)

// SchemaModifier attaches schema metadata to an atom. Pipeline boundaries use
// the explicit request-schema and response-schema header modifiers instead.
var SchemaModifier = compiler.String(SchemaModifierName,
	func(b *action.Builder[any, any], name string) *action.Builder[any, any] {
		return b.Tag("schema:" + name)
	})

var RequestSchemaModifier = compiler.String(RequestSchemaModifierName,
	func(b *action.Builder[any, any], name string) *action.Builder[any, any] {
		return b.Tag("request-schema:" + name)
	})

var ResponseSchemaModifier = compiler.String(ResponseSchemaModifierName,
	func(b *action.Builder[any, any], name string) *action.Builder[any, any] {
		return b.Tag("response-schema:" + name)
	})
