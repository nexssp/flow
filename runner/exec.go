package runner

import (
	"context"
	"os"
	"os/exec"
)

// ExecSelf re-executes the runner binary with the given args.
// The context is threaded through so the child is killed when the parent
// receives SIGTERM or its context is otherwise cancelled.
func ExecSelf(ctx context.Context, binary string, args []string) int {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}

		return 1
	}

	return 0
}
