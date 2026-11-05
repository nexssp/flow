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
