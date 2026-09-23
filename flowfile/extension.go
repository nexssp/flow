package flowfile

import "strings"

// IsFlow reports whether the given filename has a flow extension.
// Both .nflow (canonical) and .nflow (legacy) are accepted.
func IsFlow(name string) bool {
	return strings.HasSuffix(name, ".nflow")
}
