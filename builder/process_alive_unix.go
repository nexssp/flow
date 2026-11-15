//go:build unix

package builder

import (
	"errors"

	"golang.org/x/sys/unix"
)

func processAlive(pid int) (bool, error) {
	err := unix.Kill(pid, 0)
	if err == nil || errors.Is(err, unix.EPERM) {
		return true, nil
	}
	if errors.Is(err, unix.ESRCH) {
		return false, nil
	}
	return false, err
}
