//go:build windows

package external

import (
	"context"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: "cmd.exe /S /C \"" + command + "\"",
	}
	return cmd
}
