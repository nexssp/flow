//go:build windows

package capability

import (
	"context"
	"os/exec"
)

// shellCommand wraps cmdStr in `cmd /C` so that built-in shell features
// (pipes, redirection) work on Windows.
func shellCommand(ctx context.Context, cmdStr string) *exec.Cmd {
	return exec.CommandContext(ctx, "cmd", "/C", cmdStr)
}
