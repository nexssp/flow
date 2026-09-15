package runner

import (
	"strings"
)

// getAgentTag maps an action name to a short tag used as the second
// column in the live log. Pure string matching — no domain knowledge.
func getAgentTag(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "graph"):
		return "ORCHESTR"
	case strings.Contains(lower, "plan"), strings.Contains(lower, "hypothesis"):
		return "PLANNER"
	case strings.Contains(lower, "architect"), strings.Contains(lower, "code"):
		return "CODER"
	case strings.Contains(lower, "critic"), strings.Contains(lower, "review"),
		strings.Contains(lower, "judge"):
		return "CRITIC"
	case strings.Contains(lower, "projection"):
		return "MAPPER"
	case strings.Contains(lower, "sandbox"), strings.Contains(lower, "exec"),
		strings.Contains(lower, "test"), strings.Contains(lower, "experiment"):
		return "SANDBOX"
	case strings.Contains(lower, "run"):
		return "RUNNER"
	default:
		upper := strings.ToUpper(name)
		if len(upper) > 10 {
			return upper[:10]
		}

		return upper
	}
}
