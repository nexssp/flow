package at_schema

import (
	"strings"

	"github.com/nexssp/kernel/xerr"
)

// Validate checks payload against schema.
//
// The validator is deliberately shallow: it verifies that required
// fields are present and that present fields have the expected kind.
// It does not recurse into nested schemas and does not run validation
// tags (min=, max=, oneof=, …). Those belong to the action that
// consumes the schema, which has the domain knowledge to interpret
// them.
func Validate(schema Schema, payload any) error {
	m, ok := payload.(map[string]any)
	if !ok {
		return xerr.Validation("schema " + schema.Name + ": expected object")
	}

	for _, field := range schema.Fields {
		val, present := m[field.JSONName]
		required := hasValidationTag(field.Tags["validate"], "required")

		if !present {
			if required {
				return xerr.Validation(
					"schema " + schema.Name + ": field " + field.JSONName + " is required")
			}
			continue
		}

		if err := checkFieldKind(schema.Name, field, val); err != nil {
			return err
		}
	}
	return nil
}

func checkFieldKind(schemaName string, field SchemaField, val any) error {
	if field.Pointer && val == nil {
		return nil
	}

	switch {
	case field.Slice:
		if _, ok := val.([]any); !ok {
			return xerr.Validation("schema " + schemaName +
				": field " + field.JSONName + " expects slice")
		}
		return nil
	case field.Map:
		if _, ok := val.(map[string]any); !ok {
			return xerr.Validation("schema " + schemaName +
				": field " + field.JSONName + " expects map")
		}
		return nil
	}

	switch field.Kind {
	case KindString:
		if _, ok := val.(string); !ok {
			return xerr.Validation("schema " + schemaName +
				": field " + field.JSONName + " expects string")
		}
	case KindInt:
		switch val.(type) {
		case int, int64, int32, float64:
		default:
			return xerr.Validation("schema " + schemaName +
				": field " + field.JSONName + " expects number")
		}
	case KindFloat:
		switch val.(type) {
		case float64, float32, int, int64:
		default:
			return xerr.Validation("schema " + schemaName +
				": field " + field.JSONName + " expects float")
		}
	case KindBool:
		if _, ok := val.(bool); !ok {
			return xerr.Validation("schema " + schemaName +
				": field " + field.JSONName + " expects bool")
		}
	case KindStruct:
		if _, ok := val.(map[string]any); !ok {
			return xerr.Validation("schema " + schemaName +
				": field " + field.JSONName + " expects object")
		}
	}
	return nil
}

func hasValidationTag(tag, name string) bool {
	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}
