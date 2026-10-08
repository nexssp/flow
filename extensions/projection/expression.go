package projection

import (
	"strings"

	"github.com/nexssp/flow/core"
)

// buildProgramSource produces the expr-lang source string from the raw
// projection body:
//
//	{ name: .name }           →  "{ name: name }"
//	{ ..., x: 1 }             →  "__spread__(__root__, { x: 1 })"
//	{ ... }                   →  "__root__"
func buildProgramSource(raw string) string {
	state, rest, hasSpread := splitSpread(raw)
	if !hasSpread {
		return "{ " + core.PreprocessDots(raw) + " }"
	}
	if strings.TrimSpace(rest) == "" {
		return state
	}
	return "__spread__(" + state + ", { " + core.PreprocessDots(rest) + " })"
}

// splitSpread splits `..., body` into a state reference and the rest.
// Returns hasSpread=false for any content that does not start with a
// standalone spread marker.
func splitSpread(raw string) (stateRef, rest string, hasSpread bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "...") {
		return "", raw, false
	}
	after := trimmed[3:]
	if after != "" && after[0] != ',' && after[0] != ' ' && after[0] != '\t' {
		return "", raw, false
	}
	after = strings.TrimLeft(after, " \t\n")
	after = strings.TrimPrefix(after, ",")
	after = strings.TrimLeft(after, " \t\n")
	return "__root__", after, true
}
