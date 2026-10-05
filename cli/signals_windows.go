//go:build windows

package cli

import (
	"os"
	"syscall"
)

// Windows maps Ctrl-C/Break to os.Interrupt. Close, logoff, and shutdown can
// also notify SIGTERM, but the OS does not guarantee that cleanup delays exit.
func commandSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
