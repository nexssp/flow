package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

// readStringArg extracts a string field from an untyped argument. A
// non-map input or missing key returns "".
func readStringArg(in any, key string) string {
	m, ok := in.(map[string]any)
	if !ok {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

// isTruthyArg reports whether in[key] is a boolean true. Accepts bool
// values and the strings "true", "1", "yes".
func isTruthyArg(in map[string]any, key string) bool {
	value, ok := in[key]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		return err == nil && b
	}
	return false
}

// lookupDottedPath walks a nested map by a dot-separated path. The
// empty path returns the root. Missing keys return (nil, false).
func lookupDottedPath(m map[string]any, path string) (any, bool) {
	if path == "" {
		return m, true
	}
	var current any = m
	remaining := path
	for remaining != "" {
		var segment string
		if dot := strings.IndexByte(remaining, '.'); dot >= 0 {
			segment, remaining = remaining[:dot], remaining[dot+1:]
		} else {
			segment, remaining = remaining, ""
		}
		mm, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mm[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
