package projection

import (
	"context"
	"fmt"
	"reflect"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

type projectionIdentifierSpan struct {
	start int
	end   int
}

type projectionBindingReference struct {
	name   string
	offset int
}

func projectionUsedBindingReferences(source string, bindingNames []string) []projectionBindingReference {
	candidates := make(map[string]struct{}, len(bindingNames))
	for _, name := range bindingNames {
		candidates[name] = struct{}{}
	}
	if len(candidates) == 0 {
		return nil
	}

	shorthand := projectionShorthandFieldSpans(source)
	var references []projectionBindingReference
	parenDepth := 0
	for index := 0; index < len(source); {
		next, start, name, found := nextProjectionBareIdentifier(source, index, parenDepth)
		if !found {
			switch source[index] {
			case '(':
				parenDepth++
			case ')':
				if parenDepth > 0 {
					parenDepth--
				}
			}
			index = next
			continue
		}
		index = next
		if _, candidate := candidates[name]; !candidate {
			continue
		}
		if projectionIdentifierIsShorthand(start, next, shorthand) ||
			projectionIdentifierIsLocal(source, name, start, next) {
			continue
		}

		previous := previousProjectionSignificant(source, start)
		following := nextProjectionSignificant(source, next)
		if previous == '.' || previous == ']' || previous == ')' || following == ':' {
			continue
		}
		references = append(references, projectionBindingReference{name: name, offset: start})
	}
	return references
}

func projectionShorthandFieldSpans(source string) []projectionIdentifierSpan {
	var spans []projectionIdentifierSpan
	scanner := projectionShorthandScanner{
		raw:             source,
		lastSignificant: '{',
		containers:      []byte{'{'},
		bareFields:      &spans,
	}
	for index := 0; index < len(source); {
		index = scanner.rewriteAt(index)
	}
	return spans
}

func projectionIdentifierIsShorthand(start, end int, spans []projectionIdentifierSpan) bool {
	for _, span := range spans {
		if span.start == start && span.end == end {
			return true
		}
	}
	return false
}

func projectionIdentifierIsLocal(source, name string, start, end int) bool {
	position := skipProjectionTrivia(source, 0)
	for startsProjectionLet(source, position) {
		nameStart := position + len("let")
		for nameStart < len(source) && isProjectionWhitespace(source[nameStart]) {
			nameStart++
		}
		nameEnd := nameStart
		for nameEnd < len(source) && isProjectionIdentifierContinue(source[nameEnd]) {
			nameEnd++
		}

		binding, semicolon, err := parseProjectionLocalBinding(source, position)
		if err != nil {
			return false
		}
		if start >= nameStart && end <= nameEnd || binding.name == name && start > semicolon {
			return true
		}
		position = skipProjectionTrivia(source, semicolon+1)
	}
	return false
}

func validateStaticProjectionBindingConflict(source string, pos compiler.Position, ctx compiler.TypeAnalysisContext) error {
	if ctx.Input.Type == nil || ctx.Input.Stream {
		return nil
	}
	typedInputNames := projectionTypedPipelineInputNames(ctx)
	bindingNames := make([]string, 0, len(ctx.Bindings))
	if ctx.PipelineBindings != nil {
		for name := range ctx.PipelineBindings {
			if _, isPipelineInput := typedInputNames[name]; isPipelineInput {
				continue
			}
			bindingNames = append(bindingNames, name)
		}
	} else {
		for name := range ctx.Bindings {
			if _, isPipelineInput := typedInputNames[name]; isPipelineInput {
				continue
			}
			bindingNames = append(bindingNames, name)
		}
	}
	if len(bindingNames) == 0 {
		return nil
	}
	for _, reference := range projectionUsedBindingReferences(source, bindingNames) {
		inputFieldType, exists := projectionStaticInputFieldType(ctx.Input.Type, reference.name)
		if !exists {
			continue
		}
		bindingType := ctx.Bindings[reference.name]
		return compiler.SourceError(
			projectionSourcePosition(pos, source, reference.offset),
			"projection: ambiguous bare identifier %q: input field %q (type %s) conflicts with pipeline `as %s` binding (type %s); use `.%s` for the current input field or `input.%s` for the original request, or rename the binding (for example, `%s`)",
			reference.name,
			reference.name,
			inputFieldType,
			reference.name,
			projectionTypeDescription(bindingType),
			reference.name,
			reference.name,
			reference.name+"_binding",
		)
	}
	return nil
}

func projectionStaticInputFieldType(inputType reflect.Type, name string) (reflect.Type, bool) {
	for inputType != nil && inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	if inputType == nil || inputType.Kind() != reflect.Struct {
		return nil, false
	}
	fields := expressionViewFields(inputType)
	for index := range fields {
		field := &fields[index]
		if field.Tag.Get("flow_input_schema") == "true" {
			if field.JSONName == "" || projectionFieldIsBuiltin(field.JSONName) {
				continue
			}
			if field.JSONName == name || snakeCase(field.JSONName) == name {
				return field.Type, true
			}
			continue
		}
		aliases := []string{field.Name, field.ExpressionName, field.JSONName, field.SnakeName, field.ExpressionSnake}
		for _, alias := range aliases {
			if alias != "" && alias == name && !projectionFieldIsBuiltin(alias) {
				return field.Type, true
			}
		}
	}
	return nil, false
}

func projectionTypeDescription(descriptor compiler.TypeDescriptor) string {
	if descriptor.Stream {
		return "stream"
	}
	if descriptor.Type == nil {
		return "dynamic"
	}
	return descriptor.Type.String()
}

func projectionSourcePosition(base compiler.Position, source string, offset int) compiler.Position {
	position := base
	if position.Line <= 0 {
		return position
	}
	if position.Col <= 0 {
		position.Col = 1
	}
	position.Col++ // The projection body starts immediately after its opening brace.
	for index := 0; index < offset && index < len(source); index++ {
		if source[index] == '\n' {
			position.Line++
			position.Col = 1
		} else {
			position.Col++
		}
	}
	return position
}

func rejectRuntimeProjectionBindingConflict(ctx context.Context, source string, input any, cache *expressionViewTypeCache) error {
	bindingNames := compiler.ExpressionBindingNames(ctx)
	pipelineInputBindings := compiler.ExpressionPipelineInputBindingNames(ctx)
	for _, reference := range projectionUsedBindingReferences(source, bindingNames) {
		if _, isPipelineInput := pipelineInputBindings[reference.name]; isPipelineInput {
			continue
		}
		if reference.name == expressionInputName || reference.name == expressionRootName || reference.name == expressionStateName || projectionFieldIsBuiltin(reference.name) {
			continue
		}
		fields, ok := expressionValueViewWithCache(input, cache).(map[string]any)
		if !ok {
			return nil
		}
		if _, collision := fields[reference.name]; !collision {
			continue
		}
		return xerr.Conflict(fmt.Sprintf(
			"projection: ambiguous bare identifier %q: the runtime input has a field with the same name as pipeline %#q binding; use %#q for the current input field or %#q for the original request, or rename the binding (for example, %#q). The projection was not evaluated",
			reference.name,
			"as "+reference.name,
			"."+reference.name,
			"input."+reference.name,
			reference.name+"_binding",
		))
	}
	return nil
}
