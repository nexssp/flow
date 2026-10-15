package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
	schemaext "github.com/nexssp/flow/extensions/schema"
)

// materialize compiles every pipeline declared in meta and mounts it
// on the resolver.
func materialize(req compiler.MaterializeReq) error {
	pipelines, _ := req.Meta["pipelines"].(map[string]string)
	if len(pipelines) == 0 {
		return nil
	}
	if req.Compile == nil {
		return errors.New("@pipeline: compiler is not available")
	}

	pipelineMods, _ := req.Meta["pipeline_modifiers"].(map[string][]string)

	for name, source := range pipelines {
		mods := pipelineMods[name]
		schemaName, compileMods, err := splitSchemaModifier(name, mods)
		if err != nil {
			return err
		}

		canonicalName := name
		if !strings.Contains(canonicalName, ".") {
			canonicalName = "pipeline." + canonicalName
		}

		program, err := req.Compile(canonicalName, source, compileMods...)
		if err != nil {
			return fmt.Errorf("compile sub-pipeline %q: %w", name, err)
		}

		// Rename the action to its canonical name ("pipeline.<name>")
		// so it does not collide with the root atom's name ("runtime.const").
		// Dynamic preserves all modifiers, status codes, tags, and route bindings.
		var act action.AnyAction = action.Dynamic(program).Name(canonicalName).Build()
		if schemaName != "" {
			targetSchema, ok := schemaext.ByName(req.Meta, schemaName)
			if !ok {
				return fmt.Errorf("@pipeline %q: :schema=%s: unknown schema", name, schemaName)
			}
			act = wrapWithSchemaBoundary(act, schemaName, targetSchema, schemaext.SchemasFromMap(req.Meta))
		}
		if req.Introspection {
			act = action.WithComposition(act, action.IntrospectionComposition{
				Kind: "pipeline",
				Children: []action.IntrospectionChild{{
					Node: program, Relation: "pipeline_body", Role: "body", Label: canonicalName,
				}},
			})
		}

		err = req.Resolver.Mount(action.Library{
			Name: "pipeline." + name,
			Actions: []action.AnyAction{
				act,
			},
		})
		if err != nil {
			return fmt.Errorf("mount sub-pipeline %q: %w", name, err)
		}
	}
	return nil
}

func splitSchemaModifier(pipelineName string, modifiers []string) (schemaName string, compileModifiers []string, err error) {
	compileModifiers = make([]string, 0, len(modifiers))
	for _, raw := range modifiers {
		if compiler.ModifierName(raw) != "schema" {
			compileModifiers = append(compileModifiers, raw)
			continue
		}

		_, selected, hasValue := strings.Cut(raw, "=")
		if !hasValue || selected == "" {
			return "", nil, fmt.Errorf("@pipeline %q: :schema requires a name (use :schema=Name)", pipelineName)
		}
		if schemaName != "" && schemaName != selected {
			return "", nil, fmt.Errorf("@pipeline %q: conflicting :schema selections %q and %q", pipelineName, schemaName, selected)
		}
		schemaName = selected
	}
	return schemaName, compileModifiers, nil
}

func wrapWithSchemaBoundary(
	inner action.AnyAction,
	schemaName string,
	targetSchema schemaext.Schema,
	declaredSchemas []schemaext.Schema,
) action.AnyAction {
	return action.Dynamic(inner).UseFirst(func(next action.Fn[any, any]) action.Fn[any, any] {
		return func(ctx context.Context, payload any) (any, error) {
			ctx = schemaext.WithContextSchemas(ctx, declaredSchemas)
			effective, err := schemaext.ApplyDefaultsAndValidate(ctx, targetSchema, payload)
			if err != nil {
				return nil, err
			}
			return next(ctx, effective)
		}
	}).Tag("schema:" + schemaName).Build()
}
