package schema

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

func materialize(req core.MaterializeReq) error {
	schemas := SchemasFromMap(req.Meta)
	if len(schemas) == 0 {
		return nil
	}

	for _, s := range schemas {
		targetSchema := s
		canonicalName := targetSchema.Name + ".validate"

		validateAct := action.New(canonicalName, func(_ context.Context, in any) (any, error) {
			if in == nil {
				return nil, xerr.BadRequest("schema: payload is nil")
			}
			if err := Validate(targetSchema, in); err != nil {
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
