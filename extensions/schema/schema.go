package schema

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/validation"
)

const (
	typeString = "string"
	typeNumber = "number"
	typeObject = "object"
)

type Kind uint8

const (
	KindString Kind = iota
	KindInt
	KindFloat
	KindBool
	KindAny
	KindStruct
)

type Field struct {
	Name     string
	JSONName string
	Type     string
	Kind     Kind
	ElemType string
	Pointer  bool
	Slice    bool
	Map      bool
	Tags     map[string]string
	Embed    string
}

type Schema struct {
	Name   string
	Fields []Field
}

func SchemasFromMap(meta map[string]any) []Schema {
	schemas, _ := meta["schemas"].([]Schema)
	return schemas
}

func SplitFields(s Schema) (payload, config []string) {
	payload = make([]string, 0, len(s.Fields))
	config = make([]string, 0, len(s.Fields))

	for _, f := range s.Fields {
		toConfig := f.Tags["config"] == "true"
		toPayload := f.Tags["payload"] == "true" || !toConfig

		if toPayload {
			payload = append(payload, f.JSONName)
		}
		if toConfig {
			config = append(config, f.JSONName)
		}
	}
	return payload, config
}

func ByName(meta map[string]any, name string) (Schema, bool) {
	for _, s := range SchemasFromMap(meta) {
		if s.Name == name {
			return s, true
		}
	}
	return Schema{}, false
}

// Validate applies missing defaults and checks payload against the declared schema.
//
// For every field that is present, the check proceeds in two stages:
//
//  1. Go-kind check (checkFieldKind): the payload value's kind must
//     match the schema field's declared type.
//  2. Tag check (validation.Var): when the field carries a
//     `validate:"..."` tag beyond bare "required", the value is
//     validated against the tag expression using the same
//     validator/v10 instance that powers validation.Struct.
//
// The presence check for validate:"required" is handled inline, so a
// missing field reports as "is required" without invoking the
// validator. An empty string for a required field is also reported as
// "is required" because validator/v10 treats empty as the zero value.
//
// ctx is threaded from the caller so context-aware custom rules
// registered via validation.RegisterCustom see the live context, and
// so a canceled invocation aborts validation promptly.
func Validate(ctx context.Context, schema Schema, payload any) error {
	m, err := ApplyDefaults(schema, payload)
	if err != nil {
		return err
	}
	return validateValues(ctx, schema, m)
}

func validateValues(ctx context.Context, schema Schema, m map[string]any) error {
	var details xerr.ValidationDetails
	for _, field := range schema.Fields {
		value, present := m[field.JSONName]
		rule := field.Tags["validate"]
		required := hasValidationTag(rule, "required")

		if !present {
			if required {
				details = append(details, xerr.ValidationDetail{
					Field:      field.JSONName,
					Validation: "required",
					Value:      "is required",
				})
			}
			continue
		}

		if detail, bad := checkFieldKind(field, value); bad {
			details = append(details, detail)
			continue
		}

		if rule == "" {
			continue
		}
		// Bare "required" is already satisfied by presence above; every
		// other tag (including "required,email", "oneof=…", "gt=0", and
		// the plain "email", "min=3", … forms) goes through the
		// validator.
		if err := validation.Var(ctx, value, rule); err != nil {
			details = append(details, detailsFromVarError(err, field.JSONName)...)
		}
	}

	if len(details) > 0 {
		var b strings.Builder
		b.WriteString("schema ")
		b.WriteString(schema.Name)
		b.WriteString(" failed:")
		for _, d := range details {
			b.WriteString("\n    • field ")
			fmt.Fprintf(&b, "%q", d.Field)
			b.WriteByte(' ')
			if d.Value != "" {
				b.WriteString(d.Value)
			} else {
				b.WriteString("failed ")
				b.WriteString(d.Validation)
				b.WriteString(" check")
			}
		}
		return &xerr.AppError{
			Kind:              xerr.KindValidation,
			Message:           b.String(),
			ValidationDetails: details,
		}
	}
	return nil
}

// detailsFromVarError unwraps a *xerr.AppError produced by
// validation.Var and rekeys each ValidationDetail with the JSON field
// name schema.Validate was checking. Var operates on a single scalar,
// so its details carry an empty Field.
func detailsFromVarError(err error, fieldName string) xerr.ValidationDetails {
	if appErr, ok := errors.AsType[*xerr.AppError](err); ok && len(appErr.ValidationDetails) > 0 {
		out := make(xerr.ValidationDetails, 0, len(appErr.ValidationDetails))
		for _, d := range appErr.ValidationDetails {
			if d.Field == "" {
				d.Field = fieldName
			}
			out = append(out, d)
		}
		return out
	}
	return xerr.ValidationDetails{{
		Field:      fieldName,
		Validation: "custom",
		Value:      err.Error(),
	}}
}

// checkFieldKind compares the runtime kind of value against the
// schema field's declared Go kind. Each mismatch produces a
// ValidationDetail whose Value reads "must be X (got Y)".
func checkFieldKind(field Field, value any) (xerr.ValidationDetail, bool) {
	if field.Pointer && value == nil {
		return xerr.ValidationDetail{}, false
	}
	detail := xerr.ValidationDetail{Field: field.JSONName}

	switch {
	case field.Slice:
		if _, ok := value.([]any); !ok {
			detail.Validation = "slice"
			detail.Value = fmt.Sprintf("must be an array (got %s)", friendlyType(value))
			return detail, true
		}
		return detail, false
	case field.Map:
		if _, ok := value.(map[string]any); !ok {
			detail.Validation = "map"
			detail.Value = fmt.Sprintf("must be an object (got %s)", friendlyType(value))
			return detail, true
		}
		return detail, false
	}

	switch field.Kind {
	case KindString:
		if _, ok := value.(string); !ok {
			detail.Validation = typeString
			detail.Value = fmt.Sprintf("must be a string (got %s)", friendlyType(value))
			return detail, true
		}
	case KindInt, KindFloat:
		switch value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
		default:
			detail.Validation = typeNumber
			detail.Value = fmt.Sprintf("must be a number (got %s)", friendlyType(value))
			return detail, true
		}
	case KindBool:
		if _, ok := value.(bool); !ok {
			detail.Validation = "bool"
			detail.Value = fmt.Sprintf("must be a boolean (got %s)", friendlyType(value))
			return detail, true
		}
	case KindStruct:
		if _, ok := value.(map[string]any); !ok {
			detail.Validation = typeObject
			detail.Value = fmt.Sprintf("must be an object (got %s)", friendlyType(value))
			return detail, true
		}
	case KindAny:
	}

	return detail, false
}

// friendlyType names a runtime value in vocabulary the schema uses:
// "string", typeNumber, "boolean", "array", typeObject, "null". Anything
// unrecognized falls back to Go's %T.
func friendlyType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return typeNumber
	case []any:
		return "array"
	case map[string]any:
		return typeObject
	default:
		return fmt.Sprintf("%T", v)
	}
}

func hasValidationTag(tag, name string) bool {
	for part := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}

func ToJSONSchema(s Schema) map[string]any {
	properties := make(map[string]any, len(s.Fields))
	var required []string

	for _, f := range s.Fields {
		prop := map[string]any{}
		switch f.Kind {
		case KindString, KindAny:
			prop["type"] = typeString
		case KindInt, KindFloat:
			prop["type"] = typeNumber
		case KindBool:
			prop["type"] = "boolean"
		case KindStruct:
			prop["type"] = typeObject
		}

		if f.Slice {
			elemType := typeString
			switch f.Kind {
			case KindInt, KindFloat:
				elemType = typeNumber
			case KindBool:
				elemType = "boolean"
			case KindString, KindAny, KindStruct:
				elemType = typeString
			}
			prop = map[string]any{
				"type":  "array",
				"items": map[string]any{"type": elemType},
			}
		}

		if desc := f.Tags["usage"]; desc != "" {
			prop["description"] = desc
		} else if desc := f.Tags["description"]; desc != "" {
			prop["description"] = desc
		}

		if defaultValue, hasDefault, err := fieldDefaultValue(f); hasDefault && err == nil {
			prop["default"] = defaultValue
		}

		if enum := f.Tags["enum"]; enum != "" {
			raw := strings.Split(enum, ",")
			vals := make([]string, 0, len(raw))
			for _, v := range raw {
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					vals = append(vals, trimmed)
				}
			}
			if len(vals) > 0 {
				prop["enum"] = vals
			}
		}

		if hasValidationTag(f.Tags["validate"], "required") {
			required = append(required, f.JSONName)
		}

		properties[f.JSONName] = prop
	}

	out := map[string]any{
		"type":       typeObject,
		"properties": properties,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}
