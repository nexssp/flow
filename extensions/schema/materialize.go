package schema

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

func materialize(req compiler.MaterializeReq) error {
	schemas := SchemasFromMap(req.Meta)
	if err := validatePipelineSchemaModifiers(req.Meta, schemas); err != nil {
		return err
	}
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
			effective, err := ApplyDefaultsAndValidate(ctx, targetSchema, in)
			if err != nil {
				return nil, err
			}
			return effective, nil
		}).Description("Validate payload against " + targetSchema.Name).Build()

		err := req.Resolver.Mount(action.Library{
			Name:    compiler.DeclarationOwner(req.Meta, "schema", targetSchema.Name, "schema."+targetSchema.Name),
			Actions: []action.AnyAction{validateAct},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func validatePipelineSchemaModifiers(meta map[string]any, schemas []Schema) error {
	pipelineMods, _ := meta["pipeline_modifiers"].(map[string][]string)
	if len(pipelineMods) == 0 {
		return nil
	}

	declared := make(map[string]struct{}, len(schemas))
	for _, targetSchema := range schemas {
		declared[targetSchema.Name] = struct{}{}
	}

	for pipelineName, modifiers := range pipelineMods {
		for _, raw := range modifiers {
			modifierName := compiler.ModifierName(raw)
			if modifierName == SchemaModifierName {
				return fmt.Errorf("@pipeline %q: :schema is no longer supported; use :%s=Name", pipelineName, RequestSchemaModifierName)
			}
			if modifierName != RequestSchemaModifierName && modifierName != ResponseSchemaModifierName {
				continue
			}
			_, schemaName, hasValue := strings.Cut(raw, "=")
			if !hasValue || schemaName == "" {
				return fmt.Errorf("@pipeline %q: :%s requires a name (use :%s=Name)", pipelineName, modifierName, modifierName)
			}
			schemaName = strings.TrimPrefix(schemaName, "!")
			if schemaName == "" {
				return fmt.Errorf("@pipeline %q: :%s requires a schema name after the strictness marker", pipelineName, modifierName)
			}
			if _, ok := declared[schemaName]; !ok {
				return fmt.Errorf("@pipeline %q: :%s=%s: unknown schema", pipelineName, modifierName, schemaName)
			}
		}
	}
	return nil
}
