package runner

import (
	"fmt"
	"strconv"
	"strings"
)

// runnerArgs holds the flags that are the runner's concern, not the
// flow config's. Config knobs (-v, --budget=, --max-tokens=, …) are
// handled by flow.ResolveConfig and are silently ignored here.
type runnerArgs struct {
	Info       bool
	Assertions []string
	BenchNode  string
	BenchRuns  int
	CacheDir   string
	Resume     string
}

func parseRunnerArgs(args []string) runnerArgs {
	var out runnerArgs

	for _, arg := range args {
		switch {
		case arg == "-i" || arg == "--info" || arg == "info":
			out.Info = true
		case strings.HasPrefix(arg, "--assert="):
			out.Assertions = append(out.Assertions, strings.TrimPrefix(arg, "--assert="))
		case strings.HasPrefix(arg, "--testkit="):
			out.Assertions = append(out.Assertions, strings.TrimPrefix(arg, "--testkit="))
		case strings.HasPrefix(arg, "--bench="):
			out.BenchNode = strings.TrimPrefix(arg, "--bench=")
		case strings.HasPrefix(arg, "--bench-runs="):
			if n, err := strconv.Atoi(strings.TrimPrefix(arg, "--bench-runs=")); err == nil {
				out.BenchRuns = n
			}
		case strings.HasPrefix(arg, "--cache="):
			out.CacheDir = strings.TrimPrefix(arg, "--cache=")
		case strings.HasPrefix(arg, "--resume="):
			out.Resume = strings.TrimPrefix(arg, "--resume=")
		}
	}

	return out
}

// parseApprovalMode converts a config string into the typed enum.
// Unknown values are a hard error so a typo in @config:approval= does
// not silently downgrade to the default.
func parseApprovalMode(s string) (ApprovalMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "auto":
		return ApprovalNone, nil
	case "danger", "high_risk":
		return ApprovalDanger, nil
	case "all":
		return ApprovalAll, nil
	default:
		return ApprovalDanger, fmt.Errorf("unknown approval mode %q", s)
	}
}
