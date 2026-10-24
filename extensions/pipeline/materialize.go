package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
	schemaext "github.com/nexssp/flow/extensions/schema"
)

// materialize compiles every pipeline declared in meta and mounts it on the
// resolver. Declarations are sorted to make compilation and diagnostics
// deterministic; only forward references to other local pipeline declarations
// are retried.
func materialize(req compiler.MaterializeReq) error {
	pipelines, _ := req.Meta["pipelines"].(map[string]string)
	if len(pipelines) == 0 {
		return nil
	}
	if req.Compile == nil {
		return errors.New("@pipeline: compiler is not available")
	}

	pipelineMods, _ := req.Meta["pipeline_modifiers"].(map[string][]string)
	definitions := make([]pipelineDefinition, 0, len(pipelines))
	for name, source := range pipelines {
		definitions = append(definitions, pipelineDefinition{
			name:      name,
			source:    source,
			modifiers: pipelineMods[name],
		})
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].name < definitions[j].name
	})
	return materializePipelines(req, definitions)
}

type pipelineDefinition struct {
	name      string
	source    string
	modifiers []string
}

func materializePipelines(req compiler.MaterializeReq, definitions []pipelineDefinition) error {
	pending := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		pending[pipelineActionName(definition.name)] = struct{}{}
	}
	remaining := append([]pipelineDefinition(nil), definitions...)

	for len(remaining) > 0 {
		deferred := make([]pipelineDefinition, 0, len(remaining))
		deferredErrors := make([]error, 0, len(remaining))
		terminalErrors := make([]error, 0)
		madeProgress := false

		for _, definition := range remaining {
			retry, err := materializePipeline(req, definition, pending)
			if err != nil {
				if retry {
					deferred = append(deferred, definition)
					deferredErrors = append(deferredErrors, err)
				} else {
					terminalErrors = append(terminalErrors, err)
				}
				continue
			}
			delete(pending, pipelineActionName(definition.name))
			madeProgress = true
		}

		if len(terminalErrors) > 0 {
			if !madeProgress && len(deferredErrors) > 0 {
				return noProgressError(deferredErrors, terminalErrors)
			}
			return errors.Join(terminalErrors...)
		}
		if len(deferred) == 0 {
			return nil
		}
		if !madeProgress {
			return noProgressError(deferredErrors, nil)
		}
		remaining = deferred
	}
	return nil
}

func materializePipeline(req compiler.MaterializeReq, definition pipelineDefinition, pending map[string]struct{}) (retry bool, err error) {
	canonicalName := pipelineActionName(definition.name)
	requestSelection, responseSelection, compileMods, err := splitSchemaModifiers(definition.name, definition.modifiers)
	if err != nil {
		return false, err
	}

	requestSchemaValue, hasRequestSchema, err := selectedPipelineSchema(req.Meta, definition.name, requestSelection, schemaext.RequestSchemaModifierName, false)
	if err != nil {
		return false, err
	}
	responseSchemaValue, hasResponseSchema, err := selectedPipelineSchema(req.Meta, definition.name, responseSelection, schemaext.ResponseSchemaModifierName, true)
	if err != nil {
		return false, err
	}
	var requestSchema, responseSchema *schemaext.Schema
	if hasRequestSchema {
		requestSchema = &requestSchemaValue
	}
	if hasResponseSchema {
		responseSchema = &responseSchemaValue
	}

	program, err := req.Compile(canonicalName, definition.source, compileMods...)
	if err != nil {
		wrapped := fmt.Errorf("compile sub-pipeline %q: %w", definition.name, err)
		return isPendingPipelineDependency(err, pending), wrapped
	}

	// Dynamic preserves the compiled action's modifiers, statuses, tags, and
	// route bindings while giving it its canonical pipeline name.
	var act action.AnyAction = action.Dynamic(program).Name(canonicalName).Build()
	if requestSelection.name != "" || responseSelection.name != "" {
		act = wrapWithSchemaBoundary(act, requestSelection, requestSchema, responseSelection, responseSchema, schemaext.SchemasFromMap(req.Meta))
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
		Name: compiler.DeclarationOwner(req.Meta, "pipeline", canonicalName, "pipeline."+definition.name),
		Actions: []action.AnyAction{
			act,
		},
	})
	if err != nil {
		return false, fmt.Errorf("mount sub-pipeline %q: %w", definition.name, err)
	}
	return false, nil
}

func selectedPipelineSchema(meta map[string]any, pipelineName string, selection schemaSelection, modifierName string, rejectDefaults bool) (schemaext.Schema, bool, error) {
	if selection.name == "" {
		return schemaext.Schema{}, false, nil
	}
	targetSchema, ok := schemaext.ByName(meta, selection.name)
	if !ok {
		return schemaext.Schema{}, false, fmt.Errorf("@pipeline %q: :%s=%s: unknown schema", pipelineName, modifierName, selection.name)
	}
	if rejectDefaults {
		if defaultErr := rejectResponseSchemaDefaults(pipelineName, targetSchema); defaultErr != nil {
			return schemaext.Schema{}, false, defaultErr
		}
	}
	return targetSchema, true, nil
}

func noProgressError(deferredErrors, terminalErrors []error) error {
	diagnostics := make([]error, 0, len(deferredErrors)+len(terminalErrors))
	diagnostics = append(diagnostics, deferredErrors...)
	diagnostics = append(diagnostics, terminalErrors...)
	return fmt.Errorf("unable to materialize sub-pipelines (no progress): %w", errors.Join(diagnostics...))
}

func rejectResponseSchemaDefaults(pipelineName string, responseSchema schemaext.Schema) error {
	for _, field := range responseSchema.Fields {
		if _, hasDefault := field.Tags["default"]; hasDefault {
			return fmt.Errorf("@pipeline %q: :response-schema=%s: field %q declares a default; response schemas must not declare defaults", pipelineName, responseSchema.Name, field.JSONName)
		}
	}
	return nil
}

func pipelineActionName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return "pipeline." + name
}

func isPendingPipelineDependency(err error, pending map[string]struct{}) bool {
	if xerr.KindFrom(err) != xerr.KindNotFound {
		return false
	}
	var appErr *xerr.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	const prefix = "capability "
	const suffix = " not registered"
	if !strings.HasPrefix(appErr.Message, prefix) || !strings.HasSuffix(appErr.Message, suffix) {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(appErr.Message, prefix), suffix)
	_, ok := pending[name]
	return ok
}

type schemaSelection struct {
	name   string
	strict bool
}

func splitSchemaModifiers(pipelineName string, modifiers []string) (request, response schemaSelection, compileModifiers []string, err error) {
	compileModifiers = make([]string, 0, len(modifiers))
	for _, raw := range modifiers {
		modifierName := compiler.ModifierName(raw)
		if modifierName == schemaext.SchemaModifierName {
			return schemaSelection{}, schemaSelection{}, nil, fmt.Errorf("@pipeline %q: :schema is no longer supported; use :%s=Name", pipelineName, schemaext.RequestSchemaModifierName)
		}
		if modifierName != schemaext.RequestSchemaModifierName && modifierName != schemaext.ResponseSchemaModifierName {
			compileModifiers = append(compileModifiers, raw)
			continue
		}

		_, selected, hasValue := strings.Cut(raw, "=")
		if !hasValue || selected == "" {
			return schemaSelection{}, schemaSelection{}, nil, fmt.Errorf("@pipeline %q: :%s requires a name (use :%s=Name)", pipelineName, modifierName, modifierName)
		}
		selection := schemaSelection{}
		if strings.HasPrefix(selected, "!") {
			selection.strict = true
			selected = strings.TrimPrefix(selected, "!")
		}
		if selected == "" {
			return schemaSelection{}, schemaSelection{}, nil, fmt.Errorf("@pipeline %q: :%s requires a schema name after the strictness marker", pipelineName, modifierName)
		}
		selection.name = selected

		target := &request
		if modifierName == schemaext.ResponseSchemaModifierName {
			target = &response
		}
		if target.name != "" && *target != selection {
			return schemaSelection{}, schemaSelection{}, nil, fmt.Errorf("@pipeline %q: conflicting %s schema selections %q and %q", pipelineName, modifierName, target.name, selection.name)
		}
		*target = selection
	}
	return request, response, compileModifiers, nil
}

func wrapWithSchemaBoundary(
	inner action.AnyAction,
	requestSelection schemaSelection,
	requestSchema *schemaext.Schema,
	responseSelection schemaSelection,
	responseSchema *schemaext.Schema,
	declaredSchemas []schemaext.Schema,
) action.AnyAction {
	builder := action.Dynamic(inner).UseFirst(func(next action.Fn[any, any]) action.Fn[any, any] {
		return func(ctx context.Context, payload any) (any, error) {
			ctx = schemaext.WithContextSchemas(ctx, declaredSchemas)
			effective := payload
			if requestSchema != nil {
				var err error
				effective, err = schemaext.ApplyDefaultsAndValidate(ctx, *requestSchema, payload)
				if err != nil {
					return nil, err
				}
				if requestSelection.strict {
					if err := rejectUnknownFields(*requestSchema, effective); err != nil {
						return nil, err
					}
				}
			}

			result, err := next(ctx, effective)
			if err != nil {
				return nil, err
			}
			if responseSchema != nil {
				normalized, err := schemaext.NormalizeObject(result)
				if err != nil {
					return nil, err
				}
				if err := schemaext.ValidateWithoutDefaults(ctx, *responseSchema, normalized); err != nil {
					return nil, err
				}
				if responseSelection.strict {
					if err := rejectUnknownFields(*responseSchema, normalized); err != nil {
						return nil, err
					}
				}
			}
			return result, nil
		}
	})
	if requestSelection.name != "" {
		builder.Tag("request-schema:" + requestSelection.name)
	}
	if responseSelection.name != "" {
		builder.Tag("response-schema:" + responseSelection.name)
	}
	return builder.Build()
}

func rejectUnknownFields(schema schemaext.Schema, payload any) error {
	values, ok := payload.(map[string]any)
	if !ok {
		return nil // The boundary's schema validation checks object shape first.
	}
	known := make(map[string]struct{}, len(schema.Fields))
	for _, field := range schema.Fields {
		known[field.JSONName] = struct{}{}
	}
	var unknown []string
	for name := range values {
		if _, exists := known[name]; !exists {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return xerr.Validation(fmt.Sprintf("schema %s failed: unknown fields %q", schema.Name, unknown))
}
