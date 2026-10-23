package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/nexssp/flow/cli/internal/termstyle"
)

type envVariable struct {
	name    string
	purpose string
}

// envGroups is the fixed inventory of configuration inputs read directly by
// NFlow. Inherited toolchain settings and application-selected keys are excluded.
var envGroups = []struct {
	title string
	vars  []envVariable
}{
	{
		title: "NFlow configuration",
		vars: []envVariable{
			{"NFLOW_GO_VERBOSE", "1 streams Go subprocess diagnostics and disables the progress spinner; default: quiet Go output."},
			{"NFLOW_GO_TIMEOUT", "Timeout per harness Go command, as a positive Go duration (e.g. 2m); default: 10m. Invalid values warn and use the default."},
			{"NFLOW_HARNESS_CACHE", "Harness cache root; empty/unset: OS user cache directory + nflow/harness, falling back to the temporary directory + nflow/harness."},
			{"NFLOW_HARNESS", "Internal child-runner marker; any non-empty value skips external harness delegation. Default: unset; do not set manually."},
			{"LOG_LEVEL", "CLI log threshold: debug, warn, error (case-insensitive); default, empty or unrecognized: info."},
			{"LOG_FORMAT", "CLI log format: json (case-insensitive); default or any other value: text."},
		},
	},
	{
		title: "Standard variables also read directly by NFlow",
		vars: []envVariable{
			{"NO_COLOR", "Any non-empty value disables env, self bench and fs statistics colors; empty/unset: env and self bench use colors on a terminal."},
			{"GOBIN", "self up output directory; empty/unset: GOPATH/bin."},
			{"GOPATH", "self up fallback output base; empty/unset: query go env GOPATH, then use the current directory if unavailable."},
			{"GOWORK", "Workspace discovery: off disables it; an existing explicit file is used; otherwise search parent directories. Harness Go commands force off."},
			{"TEMP", "Additional system-temporary-directory check for local @require resolution; default: no additional match."},
			{"TMP", "Additional system-temporary-directory check for local @require resolution; default: no additional match."},
		},
	},
}

func init() {
	commands["env"] = func(_ context.Context, _ *invocation, args []string) int { return runEnv(args) }
	helpRegistry["env"] = helpText{
		Summary:  "Show NFlow environment settings, their set/unset state, purpose and defaults (not application secrets).",
		Usage:    "nflow env",
		Examples: []string{"nflow env"},
	}
}

func runEnv(args []string) int {
	if len(args) == 1 && wantsHelp(args) {
		PrintCommandHelp(os.Stdout, "env")
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: nflow env (no arguments; arbitrary environment lookups are not supported)")
		return 2
	}
	printEnv(os.Stdout, os.LookupEnv)
	return 0
}

func printEnv(w io.Writer, lookup func(string) (string, bool)) {
	style := termstyle.New(w)
	fmt.Fprint(w, style.Banner("ENV", "NFlow environment · current process"))
	fmt.Fprintln(w, style.Dim("  set = present (including empty values); unset = absent"))
	for _, group := range envGroups {
		fmt.Fprint(w, style.Section(group.title))
		for _, variable := range group.vars {
			value, set := lookup(variable.name)
			state, shown := style.Dim("unset"), style.Dim("-")
			if set {
				state = style.Green("set  ")
				shown = fmt.Sprintf("%q", value)
			}
			// Pad before adding ANSI escapes so columns remain aligned in a TTY.
			fmt.Fprintf(w, "  %s %s %s\n    %s\n\n", style.Bold(fmt.Sprintf("%-22s", variable.name)), state, shown, style.Dim(variable.purpose))
		}
	}
}
