package builder

import (
	"context"
	"fmt"
	"os"
)

func runFlow(args []string) int { return runExternalHarnessCommand("run", args) }

func runLint(args []string) int { return runExternalHarnessCommand("lint", args) }

func runExternalHarnessCommand(command string, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing flow path")
		return 2
	}
	path := args[0]
	reqs, err := SourceRequires(context.Background(), path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	bin, err := EnsureHarnessContext(context.Background(), reqs, path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return ExecHarness(bin, append([]string{command}, args...))
}
