package flow

import (
	"context"
	"reflect"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type CapabilitySpec struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Route        string         `json:"route,omitempty"`
	Method       string         `json:"method,omitempty"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`
	Tags         []string       `json:"tags,omitempty"`
	IsSystem     bool           `json:"is_system"`
}

type routeBindingAccessor interface {
	HTTPRoute() (method, path string)
}

func ExtractCapabilities(registry *action.Registry) []CapabilitySpec {
	if registry == nil {
		return nil
	}

	actions := registry.Actions()
	specs := make([]CapabilitySpec, 0, len(actions))

	for _, act := range actions {
		if act == nil {
			continue
		}

		meta := act.Describe()
		if meta == nil {
			continue
		}

		method, route := "", ""

		// Duck-typing avoids a direct compile dependency on specific transport packages.
		for _, binding := range act.GetBindings() {
			if accessor, ok := binding.(routeBindingAccessor); ok {
				method, route = accessor.HTTPRoute()
				break
			}

			bindingValue := reflect.ValueOf(binding)
			if bindingValue.Kind() == reflect.Struct {
				methodField := bindingValue.FieldByName("Method")
				pathField := bindingValue.FieldByName("Path")
				if methodField.IsValid() && pathField.IsValid() &&
					methodField.Kind() == reflect.String && pathField.Kind() == reflect.String {
					method = methodField.String()
					route = pathField.String()
					break
				}
			}
		}

		var reqSchema, resSchema map[string]any
		if typed, ok := act.(action.TypedPayload); ok {
			reqSchema = reflectToSchema(reflect.TypeOf(typed.ReqPayload()))
			resSchema = reflectToSchema(reflect.TypeOf(typed.ResPayload()))
		} else {
			reqSchema = map[string]any{"type": "object"}
			resSchema = map[string]any{"type": "object"}
		}

		specs = append(specs, CapabilitySpec{
			Name:         meta.Name,
			Description:  meta.Description,
			Route:        route,
			Method:       method,
			InputSchema:  reqSchema,
			OutputSchema: resSchema,
			Tags:         meta.Tags,
			IsSystem:     meta.IsSystem(),
		})
	}

	return specs
}

func BuildCatalogAction(registry *action.Registry, bindings ...action.Binding) action.AnyAction {
	builder := action.New("flow.catalog", func(_ context.Context, _ struct{}) ([]CapabilitySpec, error) {
		if registry == nil {
			return nil, xerr.NotFound("flow: registry is nil")
		}

		return ExtractCapabilities(registry), nil
	}).
		System().
		Description("Exposes machine-readable action capabilities for AI Agents & Flow DSL synthesis").
		Tag("infra", "flow", "ai")

	if len(bindings) > 0 {
		builder.Route(bindings...)
	}

	return builder.Build()
}

func reflectToSchema(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{"type": "null"}
	}

	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return map[string]any{"type": strings.ToLower(t.Kind().String())}
	}

	props := make(map[string]any)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		jsonTag := strings.Split(f.Tag.Get("json"), ",")[0]
		if jsonTag == "-" || jsonTag == "" {
			jsonTag = f.Name
		}

		valTag := f.Tag.Get("validate")
		if strings.Contains(valTag, "required") {
			required = append(required, jsonTag)
		}

		kindStr := "string"
		switch f.Type.Kind() {
		case reflect.Int, reflect.Int64, reflect.Float64:
			kindStr = "number"
		case reflect.Bool:
			kindStr = "boolean"
		case reflect.Slice:
			kindStr = "array"
		}

		props[jsonTag] = map[string]any{
			"type":       kindStr,
			"validation": valTag,
		}
	}

	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}

	return out
}
