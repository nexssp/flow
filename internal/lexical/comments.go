package lexical

import "strings"

// IsHeaderComment reports whether text begins with a supported whole-line comment marker.
func IsHeaderComment(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//")
}
