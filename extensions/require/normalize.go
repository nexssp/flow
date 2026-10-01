package require

import (
	"path"
	"strings"
)

// NormalizeID extracts the canonical bundle ID from any target path.
// e.g. "github.com/nexssp/mybundle/nexssflow" -> "mybundle"
// e.g. "nflow-harness/loose/localtest_e152bdd9" -> "localtest"
func NormalizeID(target string) string {
	clean := strings.ReplaceAll(target, `\`, "/")
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		return ""
	}
	if strings.HasPrefix(clean, "nflow-harness/loose/") {
		base := path.Base(clean)
		if idx := strings.LastIndexByte(base, '_'); idx > 0 {
			return base[:idx]
		}
		return base
	}
	base := path.Base(clean)
	if base == "." || base == "/" {
		return ""
	}
	if base == "nexssflow" {
		parent := path.Base(path.Dir(clean))
		if parent != "." && parent != "/" && parent != "" {
			return parent
		}
	}
	return base
}
