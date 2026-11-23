//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var peekNamedPipeProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

func waitForFileReadable(file *os.File, wait time.Duration) (bool, error) {
	deadline := time.Now().Add(wait)
	for {
		var available uint32
		result, _, callErr := peekNamedPipeProc.Call(
			file.Fd(),
			0,
			0,
			0,
			uintptr(unsafe.Pointer(&available)),
			0,
		)
		runtime.KeepAlive(&available)
		if result == 0 {
			if errors.Is(callErr, windows.ERROR_BROKEN_PIPE) {
				return true, nil
			}
			return false, fmt.Errorf("check stdin pipe readiness: %w", callErr)
		}
		if available > 0 {
			return true, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		if remaining > time.Millisecond {
			remaining = time.Millisecond
		}
		time.Sleep(remaining)
	}
}
