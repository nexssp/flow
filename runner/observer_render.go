package runner

import (
	"fmt"
	"strings"

	"github.com/nexssp/ai/agent/eval"
	aiflow "github.com/nexssp/ai/flow"
)

// getAgentTag maps an action name to a short tag used as the second
// column in the live log. Keeps the log scannable at a glance.
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

// getResultSummary formats a human-readable one-liner for a successful
// action result. Falls back to a generic message for unknown types.
func getResultSummary(res any) string {
	if res == nil {
		return "Task completed successfully."
	}

	switch v := res.(type) {
	case aiflow.PlanResult:
		return fmt.Sprintf("Decomposed goal into %d actionable steps.", len(v.Steps))
	case aiflow.CodeResult:
		lines := strings.Count(v.SourceCode, "\n") + 1

		return fmt.Sprintf("Generated source code artifact (%d lines).", lines)
	case aiflow.SandboxExecRes:
		status := "✅"
		if v.ExitCode != 0 {
			status = "❌"
		}

		msg := fmt.Sprintf("Container exit %d %s", v.ExitCode, status)
		if v.Stderr != "" {
			msg += fmt.Sprintf("\n              ├── ⚠️ STDERR: %s", snip(v.Stderr, 100))
		}

		return msg
	case eval.Verdict:
		status := "Rejected"
		if v.Approved {
			status = "Approved"
		}

		msg := fmt.Sprintf("%s. Score: %d/100.", status, v.Score)
		if len(v.Defects) > 0 {
			msg += fmt.Sprintf(" Found %d defects.", len(v.Defects))
		}

		return msg
	default:
		return "Task completed."
	}
}
