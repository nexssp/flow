//go:build !windows

package capability

import (
	"context"
	"os/exec"
)

// shellCommand wraps cmdStr in `sh -c` so pipelines, redirects, and globs
// work as expected on POSIX systems.
func shellCommand(ctx context.Context, cmdStr string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", cmdStr)
}
