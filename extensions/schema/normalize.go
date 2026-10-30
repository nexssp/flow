package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/nexssp/kernel/xerr"
)

// NormalizeObject returns the JSON-visible fields of value as a generic object.
// It follows encoding/json semantics for field visibility, json tags, embedded
// fields, pointers, custom marshalers, and omitempty. Nested objects become
// map[string]any, arrays become []any, and JSON numbers become float64. Native
// time.Duration values are restored in the decoded view so duration schemas can
// validate them without a JSON-number round trip.
//
// A nil value, nil pointer, or value that encodes as a non-object is rejected.
// The input is not modified. Use the returned view for inspection or
// validation; retain the original value when typed output must be preserved.
func NormalizeObject(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, xerr.Validation(fmt.Sprintf("schema: encode object: %v", err))
	}
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, xerr.Validation(fmt.Sprintf("schema: expected object, got %T", value))
	}

	var normalized map[string]any
	if err := json.Unmarshal(trimmed, &normalized); err != nil {
		return nil, xerr.Validation(fmt.Sprintf("schema: decode object: %v", err))
	}
	normalized, _ = restoreTypedDurations(normalized, reflect.ValueOf(value)).(map[string]any)
	return normalized, nil
}

var durationType = reflect.TypeFor[time.Duration]()

func restoreTypedDurations(normalized any, source reflect.Value) any {
	source = indirectValue(source)
	if !source.IsValid() {
		return normalized
	}
	if source.Type() == durationType {
		if source.CanInterface() {
			return source.Interface()
		}
		return normalized
	}

	switch target := normalized.(type) {
	case map[string]any:
		for name, child := range target {
			if sourceChild, ok := jsonFieldValue(source, name); ok {
				target[name] = restoreTypedDurations(child, sourceChild)
			}
		}
	case []any:
		if source.Kind() == reflect.Slice || source.Kind() == reflect.Array {
			limit := min(len(target), source.Len())
			for i := range limit {
				target[i] = restoreTypedDurations(target[i], source.Index(i))
			}
		}
	}
	return normalized
}

func indirectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func jsonFieldValue(source reflect.Value, name string) (reflect.Value, bool) {
	source = indirectValue(source)
	if !source.IsValid() {
		return reflect.Value{}, false
	}
	if source.Kind() == reflect.Map {
		key := reflect.ValueOf(name)
		if !key.Type().ConvertibleTo(source.Type().Key()) {
			return reflect.Value{}, false
		}
		value := source.MapIndex(key.Convert(source.Type().Key()))
		return value, value.IsValid()
	}
	if source.Kind() == reflect.Struct {
		for i := range source.NumField() {
			field := source.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			tagName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if tagName == "-" {
				continue
			}
			fieldValue := source.Field(i)
			if field.Anonymous && tagName == "" {
				if embedded, ok := jsonFieldValue(fieldValue, name); ok {
					return embedded, true
				}
				continue
			}
			if tagName == "" {
				tagName = field.Name
			}
			if tagName == name {
				return fieldValue, true
			}
		}
	}
	return reflect.Value{}, false
}
