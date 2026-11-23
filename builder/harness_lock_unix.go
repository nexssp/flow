//go:build unix

package builder

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"golang.org/x/sys/unix"
)

func lockHarnessFile(file *os.File) (unlock func(), supported bool, err error) {
	fd := file.Fd()
	if fd > uintptr(^uint(0)>>1) {
		return nil, true, fmt.Errorf("file descriptor %d exceeds int range", fd)
	}
	if err := unix.Flock(int(fd), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, true, err
	}
	return func() {
		if err := unix.Flock(int(fd), unix.LOCK_UN); err != nil {
			slog.Warn("failed to release harness file lock", "error", err)
		}
	}, true, nil
}

func harnessLockBusy(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
}
