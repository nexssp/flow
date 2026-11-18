package schema

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/validation"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/secret"
)

const (
	typeString   = "string"
	typeNumber   = "number"
	typeObject   = "object"
	typeDuration = "duration"
)

type Kind uint8

const (
	KindString Kind = iota
	KindInt
	KindFloat
	KindBool
	KindDuration
	KindAny
	KindStruct
	KindSecret
)

type Field struct {
	Name            string
	JSONName        string
	Type            string
	Kind            Kind
	ElemType        string
	Pointer         bool
	Slice           bool
	Map             bool
	Tags            map[string]string
	Embed           string
	EnumType        string
	EnumValues      []string
	TypedDefault    any
	HasTypedDefault bool
	NestedSchema    *Schema `json:"-"`
	Position        compiler.Position
}

type Schema struct {
	Name   string
	Fields []Field
}

func SchemasFromMap(meta map[string]any) []Schema {
	schemas, _ := meta["schemas"].([]Schema)
	return schemas
}

// VisibleSchemasFromMeta returns local schemas plus schemas from explicitly
// included modules, qualified by each include alias. It does not merge imported
// schemas into the local declaration set; callers must use the qualified name
// (for example, memory.ChatTurn) to select an imported schema.
func VisibleSchemasFromMeta(meta map[string]any) []Schema {
	visible := append([]Schema(nil), SchemasFromMap(meta)...)
	for _, imported := range compiler.SourceImports(meta) {
		if imported.Source == nil {
			continue
		}
		for _, target := range VisibleSchemasFromMeta(imported.Source.Metadata()) {
			target.Name = imported.Namespace + "." + target.Name
			visible = append(visible, target)
		}
	}
	return visible
}

func SplitFields(s Schema) (payload, config []string) {
	payload = make([]string, 0, len(s.Fields))
	config = make([]string, 0, len(s.Fields))

	for i := range s.Fields {
		f := s.Fields[i]
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
	for _, s := range VisibleSchemasFromMeta(meta) {
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
	_, err := ApplyDefaultsAndValidate(ctx, schema, payload)
	return err
}

// ValidateWithoutDefaults checks a payload against a schema without adding
// default values or changing the payload. Unknown fields are left untouched.
func ValidateWithoutDefaults(ctx context.Context, schema Schema, payload any) error {
	values, ok := payload.(map[string]any)
	if !ok {
		return xerr.Validation(fmt.Sprintf("schema %s: expected object, got %T", schema.Name, payload))
	}
	if err := validateValues(ctx, schema, values); err != nil {
		return err
	}
	return validateNestedFields(ctx, schema, values, false)
}

// ApplyDefaultsAndValidate returns the effective payload after applying
// missing defaults and validating the declared fields. Explicit input values,
// including typed slice values, are preserved.
func ApplyDefaultsAndValidate(ctx context.Context, schema Schema, payload any) (map[string]any, error) {
	m, err := ApplyDefaults(schema, payload)
	if err != nil {
		return nil, err
	}
	if err := validateNestedFields(ctx, schema, m, true); err != nil {
		return nil, err
	}
	if err := normalizeSecretFields(schema, m); err != nil {
		return nil, err
	}
	if err := validateValues(ctx, schema, m); err != nil {
		return nil, err
	}
	return m, nil
}

// validateNestedFields recursively checks fields whose type names an earlier
// @schema declaration. In defaulting mode it replaces nested values with their
// effective maps so nested defaults are visible to downstream actions.
func validateNestedFields(ctx context.Context, schema Schema, values map[string]any, applyDefaults bool) error {
	for index := range schema.Fields {
		field := &schema.Fields[index]
		if field.NestedSchema == nil {
			continue
		}
		value, present := values[field.JSONName]
		if !present {
			continue
		}
		if field.Slice {
			items, ok := sliceValue(value)
			if !ok {
				continue // The field-kind check reports the container mismatch.
			}
			effective := make([]any, len(items))
			for index, item := range items {
				object, err := nestedObject(ctx, schema, field, item, fmt.Sprintf("%s[%d]", field.JSONName, index), applyDefaults)
				if err != nil {
					return err
				}
				effective[index] = object
			}
			if applyDefaults {
				values[field.JSONName] = preserveNestedSliceType(value, effective)
			}
			continue
		}
		object, isObject := value.(map[string]any)
		if !isObject {
			continue // The field-kind check reports the object mismatch.
		}
		effective, err := nestedObject(ctx, schema, field, object, field.JSONName, applyDefaults)
		if err != nil {
			return err
		}
		if applyDefaults {
			values[field.JSONName] = effective
		}
	}
	return nil
}

func nestedObject(ctx context.Context, parent Schema, field *Field, value any, path string, applyDefaults bool) (map[string]any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, xerr.Validation(fmt.Sprintf("schema %s: field %q must be an object (got %s)", parent.Name, path, friendlyType(value)))
	}
	var (
		effective map[string]any
		err       error
	)
	if applyDefaults {
		effective, err = ApplyDefaultsAndValidate(ctx, *field.NestedSchema, object)
	} else {
		err = ValidateWithoutDefaults(ctx, *field.NestedSchema, object)
		effective = object
	}
	if err != nil {
		return nil, fmt.Errorf("schema %s field %q: %w", parent.Name, path, err)
	}
	return effective, nil
}

func sliceValue(value any) ([]any, bool) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || reflected.Kind() != reflect.Slice {
		return nil, false
	}
	items := make([]any, reflected.Len())
	for index := range reflected.Len() {
		items[index] = reflected.Index(index).Interface()
	}
	return items, true
}

func preserveNestedSliceType(original any, values []any) any {
	reflected := reflect.ValueOf(original)
	if !reflected.IsValid() || reflected.Kind() != reflect.Slice {
		return values
	}
	result := reflect.MakeSlice(reflected.Type(), len(values), len(values))
	for index, value := range values {
		item := reflect.ValueOf(value)
		if !item.IsValid() || !item.Type().AssignableTo(reflected.Type().Elem()) {
			return values
		}
		result.Index(index).Set(item)
	}
	return result.Interface()
}

func validateValues(ctx context.Context, schema Schema, m map[string]any) error {
	var details xerr.ValidationDetails
	for i := range schema.Fields {
		field := schema.Fields[i]
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
		if detail, bad := checkEnumMembership(field, value); bad {
			details = append(details, detail)
			continue
		}
		if field.Kind == KindSecret {
			secretValue, isSecret := value.(secret.Value)
			if required && (!isSecret || secret.IsEmpty(secretValue)) {
				details = append(details, xerr.ValidationDetail{Field: field.JSONName, Validation: "required", Value: "is required"})
			}
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

func normalizeSecretFields(schema Schema, values map[string]any) error {
	for index := range schema.Fields {
		field := &schema.Fields[index]
		if field.Kind != KindSecret {
			continue
		}
		value, present := values[field.JSONName]
		if !present {
			continue
		}
		if typed, ok := value.(secret.Value); ok {
			values[field.JSONName] = typed
			continue
		}
		text, ok := value.(string)
		if !ok {
			return xerr.Validation(fmt.Sprintf("schema %s field %q must be a secret string", schema.Name, field.JSONName))
		}
		values[field.JSONName] = secret.New(text)
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

func checkEnumMembership(field Field, value any) (xerr.ValidationDetail, bool) {
	if len(field.EnumValues) == 0 || value == nil {
		return xerr.ValidationDetail{}, false
	}
	allowed := func(candidate any) (string, bool) {
		text, ok := candidate.(string)
		if !ok {
			return text, false
		}
		return text, slices.Contains(field.EnumValues, text)
	}
	if field.Slice {
		reflected := reflect.ValueOf(value)
		for index := range reflected.Len() {
			candidate := reflected.Index(index).Interface()
			text, ok := allowed(candidate)
			if !ok {
				detail := xerr.ValidationDetail{Field: field.JSONName, Validation: "enum"}
				if _, isString := candidate.(string); !isString {
					detail.Value = fmt.Sprintf("element %d must be an enum string (got %s)", index, friendlyType(candidate))
				} else {
					detail.Value = fmt.Sprintf("element %d must be one of %v (got %q)", index, field.EnumValues, text)
				}
				return detail, true
			}
		}
		return xerr.ValidationDetail{}, false
	}
	text, ok := allowed(value)
	if ok {
		return xerr.ValidationDetail{}, false
	}
	detail := xerr.ValidationDetail{Field: field.JSONName, Validation: "enum"}
	if _, isString := value.(string); !isString {
		detail.Value = fmt.Sprintf("must be an enum string (got %s)", friendlyType(value))
	} else {
		detail.Value = fmt.Sprintf("must be one of %v (got %q)", field.EnumValues, text)
	}
	return detail, true
}

// checkFieldKind compares the runtime kind of value against the
// schema field's declared Go kind. Each mismatch produces a
// ValidationDetail whose Value reads "must be X (got Y)".
func ValidateScalarValue(typeName string, value any) error {
	if !IsScalarTypeName(typeName) {
		return xerr.BadRequest(fmt.Sprintf("schema: unsupported scalar type %q", typeName))
	}
	field := Field{Type: typeName, JSONName: "item"}
	classifyFieldType(&field)
	if detail, bad := checkFieldKind(field, value); bad {
		return xerr.Validation("schema array item " + detail.Value)
	}
	return nil
}

//nolint:gocyclo // The validator reports a distinct typed diagnostic for each supported schema kind.
func checkFieldKind(field Field, value any) (xerr.ValidationDetail, bool) {
	if field.Pointer && value == nil {
		return xerr.ValidationDetail{}, false
	}
	detail := xerr.ValidationDetail{Field: field.JSONName}

	switch {
	case field.Slice:
		// Preserve the caller's concrete slice type. Element constraints are
		// handled by validation tags such as `dive,required` below.
		reflected := reflect.ValueOf(value)
		if !reflected.IsValid() || reflected.Kind() != reflect.Slice {
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
	case KindDuration:
		if _, ok := value.(time.Duration); !ok {
			detail.Validation = typeDuration
			detail.Value = fmt.Sprintf("must be a duration (got %s)", friendlyType(value))
			return detail, true
		}
	case KindSecret:
		if _, ok := value.(secret.Value); !ok {
			detail.Validation = "secret"
			detail.Value = fmt.Sprintf("must be a secret value (got %s)", friendlyType(value))
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
	if reflect.ValueOf(v).Kind() == reflect.Slice {
		return "array"
	}
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case time.Duration:
		return typeDuration
	case secret.Value:
		return "secret"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return typeNumber
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

	for i := range s.Fields {
		f := s.Fields[i]
		prop := jsonSchemaKind(f.Kind)

		if f.Slice {
			prop = map[string]any{
				"type":  "array",
				"items": prop,
			}
		}

		if desc := f.Tags["usage"]; desc != "" {
			prop["description"] = desc
		} else if desc := f.Tags["description"]; desc != "" {
			prop["description"] = desc
		}
		if f.Kind == KindSecret {
			prop["writeOnly"] = true
			prop["format"] = "password"
		}

		if defaultValue, hasDefault, err := fieldDefaultValue(f); hasDefault && err == nil {
			prop["default"] = defaultValue
		}

		if len(f.EnumValues) > 0 {
			if f.Slice {
				if items, ok := prop["items"].(map[string]any); ok {
					items["enum"] = append([]string(nil), f.EnumValues...)
				}
			} else {
				prop["enum"] = append([]string(nil), f.EnumValues...)
			}
		} else if enum := f.Tags["enum"]; enum != "" {
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

func jsonSchemaKind(kind Kind) map[string]any {
	prop := map[string]any{}
	switch kind {
	case KindString:
		prop["type"] = typeString
	case KindSecret:
		prop["type"] = typeString
	case KindInt, KindFloat:
		prop["type"] = typeNumber
	case KindBool:
		prop["type"] = "boolean"
	case KindDuration:
		prop["type"] = "integer"
	case KindStruct:
		prop["type"] = typeObject
	case KindAny:
		// Any accepts all JSON types, so it must not carry a type restriction.
	}
	return prop
}
