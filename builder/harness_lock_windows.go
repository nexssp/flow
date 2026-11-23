//go:build windows

package builder

import (
	"errors"
	"log/slog"
	"os"

	"golang.org/x/sys/windows"
)

func lockHarnessFile(file *os.File) (unlock func(), supported bool, err error) {
	overlapped := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped); err != nil {
		return nil, true, err
	}
	return func() {
		if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped); err != nil {
			slog.Warn("failed to release harness file lock", "error", err)
		}
	}, true, nil
}

func harnessLockBusy(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
