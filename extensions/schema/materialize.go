package schema

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

func materialize(req compiler.MaterializeReq) error {
	schemas := SchemasFromMap(req.Meta)
	if len(schemas) == 0 {
		return nil
	}

	for _, s := range schemas {
		targetSchema := s
		canonicalName := targetSchema.Name + ".validate"

		// was: func(_ context.Context, in any)
		validateAct := action.New(canonicalName, func(ctx context.Context, in any) (any, error) {
			if in == nil {
				return nil, xerr.BadRequest("schema: payload is nil")
			}
			if err := Validate(ctx, targetSchema, in); err != nil {
				return nil, err
			}
			return in, nil
		}).Description("Validate payload against " + targetSchema.Name).Build()

		err := req.Resolver.Mount(action.Library{
			Name:    "schema." + targetSchema.Name,
			Actions: []action.AnyAction{validateAct},
		})
		if err != nil {
			return err
		}
	}
	return nil
}
