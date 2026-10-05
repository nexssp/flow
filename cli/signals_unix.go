//go:build unix

package cli

import (
	"os"
	"syscall"
)

func commandSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
