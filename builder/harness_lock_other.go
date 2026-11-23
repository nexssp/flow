//go:build !unix && !windows

package builder

import (
	"errors"
	"os"
)

var errHarnessLockUnsupported = errors.New("advisory file locks are unsupported on this platform")

func lockHarnessFile(*os.File) (unlock func(), supported bool, err error) {
	return nil, false, errHarnessLockUnsupported
}

func harnessLockBusy(error) bool { return false }
