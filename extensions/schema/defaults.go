package schema

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/xerr"
)

// ApplyDefaults returns a copy of payload with each missing field filled from
// its schema field's `default` tag. Explicitly supplied values, including nil,
// are preserved. The input map is never modified.
//
// Scalar defaults are parsed according to the declared field type. Slice,
// map, and struct defaults use JSON values, for example `default:"[1,2]"` or
// `default:"{\"enabled\":true}"`.
func ApplyDefaults(schema Schema, payload any) (map[string]any, error) {
	input, ok := payload.(map[string]any)
	if !ok {
		return nil, xerr.Validation(fmt.Sprintf("schema %s: expected object, got %T", schema.Name, payload))
	}

	values := make(map[string]any, len(input)+len(schema.Fields))
	maps.Copy(values, input)

	for _, field := range schema.Fields {
		if _, present := values[field.JSONName]; present {
			continue
		}
		value, hasDefault, err := fieldDefaultValue(field)
		if err != nil {
			return nil, fmt.Errorf("schema %s field %q default: %w", schema.Name, field.JSONName, err)
		}
		if hasDefault {
			values[field.JSONName] = value
		}
	}
	return values, nil
}

// fieldDefaultValue parses a field's default tag into the generic runtime
// representation used by schema validation and payload maps.
func fieldDefaultValue(field Field) (value any, hasDefault bool, err error) {
	raw, ok := field.Tags["default"]
	if !ok {
		return nil, false, nil
	}
	value, err = parseDefaultValue(field, raw)
	if err != nil {
		return nil, true, err
	}
	return value, true, nil
}

func parseDefaultValue(field Field, raw string) (any, error) {
	if field.Slice || field.Map {
		value, err := parseJSONDefault(raw)
		if err != nil {
			return nil, fmt.Errorf("must be valid JSON: %w", err)
		}
		if field.Slice {
			if _, ok := value.([]any); !ok {
				return nil, errorsForDefaultType("a JSON array", value)
			}
			return value, nil
		}
		if _, ok := value.(map[string]any); !ok {
			return nil, errorsForDefaultType("a JSON object", value)
		}
		return value, nil
	}

	typeName := strings.TrimPrefix(strings.TrimSpace(field.Type), "*")
	switch field.Kind {
	case KindString:
		return raw, nil
	case KindInt:
		return parseIntegerDefault(typeName, raw)
	case KindFloat:
		bits := 64
		if typeName == "float32" {
			bits = 32
		}
		value, err := strconv.ParseFloat(raw, bits)
		if err != nil {
			return nil, err
		}
		if bits == 32 {
			return float32(value), nil
		}
		return value, nil
	case KindBool:
		return strconv.ParseBool(raw)
	case KindAny:
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			return value, nil
		}
		return raw, nil
	case KindStruct:
		return parseJSONObjectDefault(raw)
	default:
		return nil, fmt.Errorf("defaults are not supported for field type %q", field.Type)
	}
}

func parseIntegerDefault(typeName, raw string) (any, error) {
	if strings.HasPrefix(typeName, "uint") {
		bits := integerBitSize(typeName)
		value, err := strconv.ParseUint(raw, 10, bits)
		if err != nil {
			return nil, err
		}
		switch typeName {
		case "uint":
			return uint(value), nil
		case "uint8":
			// ParseUint enforces the destination width before this conversion.
			//nolint:gosec // G115: conversion is range-checked by ParseUint.
			return uint8(value), nil
		case "uint16":
			//nolint:gosec // G115: conversion is range-checked by ParseUint.
			return uint16(value), nil
		case "uint32":
			//nolint:gosec // G115: conversion is range-checked by ParseUint.
			return uint32(value), nil
		default:
			return value, nil
		}
	}

	bits := integerBitSize(typeName)
	value, err := strconv.ParseInt(raw, 10, bits)
	if err != nil {
		return nil, err
	}
	switch typeName {
	case "int8":
		// ParseInt enforces the destination width before this conversion.
		//nolint:gosec // G115: conversion is range-checked by ParseInt.
		return int8(value), nil
	case "int16":
		//nolint:gosec // G115: conversion is range-checked by ParseInt.
		return int16(value), nil
	case "int32":
		//nolint:gosec // G115: conversion is range-checked by ParseInt.
		return int32(value), nil
	case "int64":
		return value, nil
	default:
		return int(value), nil
	}
}

func integerBitSize(typeName string) int {
	switch typeName {
	case "int8", "uint8":
		return 8
	case "int16", "uint16":
		return 16
	case "int32", "uint32":
		return 32
	case "int64", "uint64":
		return 64
	default:
		return strconv.IntSize
	}
}

func parseJSONDefault(raw string) (any, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func parseJSONObjectDefault(raw string) (any, error) {
	value, err := parseJSONDefault(raw)
	if err != nil {
		return nil, fmt.Errorf("must be valid JSON: %w", err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, errorsForDefaultType("a JSON object", value)
	}
	return value, nil
}

func errorsForDefaultType(want string, value any) error {
	return fmt.Errorf("must be %s (got %s)", want, friendlyType(value))
}
