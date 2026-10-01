package schema

import "fmt"

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
		value := raw[valueStart:i]
		i++

		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate tag %q", key)
		}
		out[key] = value
	}
	return out, nil
}

func isValidSchemaName(s string) bool {
	if s == "" {
		return false
	}
	first := s[0]
	if first != '_' && (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
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
