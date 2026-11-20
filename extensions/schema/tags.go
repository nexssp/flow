package schema

import (
	"fmt"
	"strconv"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// parseStructTags parses the tag literal between backticks:
//
//	json:"name" validate:"required"
//
// Only the `key:"value"` form is supported; malformed input is an
// error.
func parseStructTags(raw string) (map[string]string, error) {
	out := map[string]string{}
	i := 0
	for i < len(raw) {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
			i++
		}
		if i >= len(raw) {
			break
		}

		keyStart := i
		for i < len(raw) && raw[i] != ':' && raw[i] != ' ' {
			i++
		}
		if i >= len(raw) || raw[i] != ':' {
			return nil, fmt.Errorf("expected ':' after key at offset %d", keyStart)
		}
		key := raw[keyStart:i]
		i++

		if i >= len(raw) || raw[i] != '"' {
			return nil, fmt.Errorf("expected '\"' after key %q", key)
		}
		i++
		valueStart := i
		for i < len(raw) && raw[i] != '"' {
			if raw[i] == '\\' && i+1 < len(raw) {
				i += 2
				continue
			}
			i++
		}
		if i >= len(raw) {
			return nil, fmt.Errorf("unclosed value for tag %q", key)
		}
		value, err := strconv.Unquote(raw[valueStart-1 : i+1])
		if err != nil {
			return nil, fmt.Errorf("invalid quoted value for tag %q: %w", key, err)
		}
		i++

		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate tag %q", key)
		}
		out[key] = value
	}
	return out, nil
}

func validateFieldValidationTag(field *Field) error {
	raw, ok := field.Tags["validate"]
	if !ok || raw == "" {
		return nil
	}
	for index, rule := range strings.Split(raw, ",") {
		if strings.TrimSpace(rule) == "" {
			return compiler.SourceError(field.Position,
				"field %q: validate tag has an empty rule at segment %d", field.Name, index+1)
		}
	}
	return nil
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'A' && c <= 'Z' {
		return string(c+32) + s[1:]
	}
	return s
}
