package schema

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/xerr"
)

// SchemaKind is a coarse classification used by Validate.
type Kind uint8

const (
	KindString Kind = iota
	KindInt
	KindFloat
	KindBool
	KindAny
	KindStruct
)

// SchemaField is one parsed field declaration.
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
	// Embed is set only while a @schema body is being parsed: a line with
	// a single identifier is a composition (embed) of another declared
	// schema, not a field. Resolved schemas never carry Embed.
	Embed string
}

// Schema is one complete declaration.
type Schema struct {
	Name   string
	Fields []Field
}

// SchemasFromMap returns the schemas stored under meta["schemas"].
// Nil-safe.
func SchemasFromMap(meta map[string]any) []Schema {
	schemas, _ := meta["schemas"].([]Schema)
	return schemas
}

// SplitFields classifies schema fields by their transport tags.
//
//	A field with config:"true" goes only to compile config unless it
//	also declares payload:"true".
//	A field without config:"true" goes only to payload unless it
//	declares config:"true" without payload.
//
// A field without either tag defaults to payload. A field with both
// goes to both channels. Any tag value other than "true" is treated
// as absent. Result slices preserve declaration order.
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

// SchemaByName returns the named schema if declared.
func ByName(meta map[string]any, name string) (Schema, bool) {
	for _, s := range SchemasFromMap(meta) {
		if s.Name == name {
			return s, true
		}
	}
	return Schema{}, false
}

// Validate checks payload against schema. It only enforces required
// fields and coarse type compatibility; richer validation is the
// job of the validation package.
func Validate(schema Schema, payload any) error {
	m, ok := payload.(map[string]any)
	if !ok {
		return xerr.Validation(fmt.Sprintf("schema %s: expected object, got %T", schema.Name, payload))
	}

	var details xerr.ValidationDetails
	for _, field := range schema.Fields {
		value, present := m[field.JSONName]
		required := hasValidationTag(field.Tags["validate"], "required")

		if !present {
			if required {
				details = append(details, xerr.ValidationDetail{
					Field:      field.JSONName,
					Validation: "required",
				})
			}
			continue
		}
		if detail, bad := checkFieldKind(field, value); bad {
			details = append(details, detail)
		}
	}

	if len(details) > 0 {
		return &xerr.AppError{
			Kind:              xerr.KindValidation,
			Message:           "schema " + schema.Name + ": validation failed",
			ValidationDetails: details,
		}
	}
	return nil
}

// checkFieldKind returns a ValidationDetail describing the mismatch,
// or ok=false when the value is compatible.
func checkFieldKind(field Field, value any) (xerr.ValidationDetail, bool) {
	if field.Pointer && value == nil {
		return xerr.ValidationDetail{}, false
	}
	detail := xerr.ValidationDetail{Field: field.JSONName}

	switch {
	case field.Slice:
		if _, ok := value.([]any); !ok {
			detail.Validation = "slice"
			detail.Value = fmt.Sprintf("got %T", value)
			return detail, true
		}
		return detail, false
	case field.Map:
		if _, ok := value.(map[string]any); !ok {
			detail.Validation = "map"
			detail.Value = fmt.Sprintf("got %T", value)
			return detail, true
		}
		return detail, false
	}

	switch field.Kind {
	case KindString:
		if _, ok := value.(string); !ok {
			detail.Validation = "string"
			detail.Value = fmt.Sprintf("got %T", value)
			return detail, true
		}
	case KindInt, KindFloat:
		switch value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
		default:
			detail.Validation = "number"
			detail.Value = fmt.Sprintf("got %T", value)
			return detail, true
		}
	case KindBool:
		if _, ok := value.(bool); !ok {
			detail.Validation = "bool"
			detail.Value = fmt.Sprintf("got %T", value)
			return detail, true
		}
	case KindStruct:
		if _, ok := value.(map[string]any); !ok {
			detail.Validation = "object"
			detail.Value = fmt.Sprintf("got %T", value)
			return detail, true
		}
	case KindAny:
	}

	return detail, false
}

func hasValidationTag(tag, name string) bool {
	for part := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}
