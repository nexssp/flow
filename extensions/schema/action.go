package schema

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
)

var schemasContextKey = xctx.NewKey[map[string]Schema]("flow.schemas.active")

func WithContextSchemas(ctx context.Context, schemas []Schema) context.Context {
	if len(schemas) == 0 {
		return ctx
	}
	m := make(map[string]Schema, len(schemas))
	for _, s := range schemas {
		m[s.Name] = s
	}
	return schemasContextKey.With(ctx, m)
}

type ValidateRequest struct {
	Name    string `json:"name"              validate:"required"`
	Payload any    `json:"payload,omitempty"`
}

var ValidateAction = action.New("schema.validate", func(ctx context.Context, req ValidateRequest) (any, error) {
	if req.Name == "" {
		return nil, xerr.BadRequest("schema.validate: schema name is required (use @{ name: \"SchemaName\" })")
	}

	schemas, _ := schemasContextKey.From(ctx)
	targetSchema, ok := schemas[req.Name]
	if !ok {
		return nil, xerr.NotFound("schema.validate: schema '" + req.Name + "' not registered in workflow")
	}

	targetPayload := req.Payload
	if targetPayload == nil {
		targetPayload, _ = rootInputKey.From(ctx)
	}

	if targetPayload == nil {
		return nil, xerr.BadRequest("schema.validate: no payload available to validate")
	}

	// CHANGED: ctx is now threaded through so validation.Var (called
	// from schema.Validate) can honor context cancellation and pass
	// ctx to any context-aware custom rules registered via
	// validation.RegisterCustom.
	effective, err := ApplyDefaultsAndValidate(ctx, targetSchema, targetPayload)
	if err != nil {
		return nil, err
	}

	return effective, nil
}).Description("Validate payload against a declared @schema").
	Tag("schema", "validation").
	Build()
