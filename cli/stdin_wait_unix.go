//go:build unix

package cli

import (
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func waitForFileReadable(file *os.File, wait time.Duration) (bool, error) {
	fd := file.Fd()
	if fd > uintptr(math.MaxInt32) {
		return false, fmt.Errorf("stdin file descriptor %d exceeds poll range", fd)
	}
	deadline := time.Now().Add(wait)
	pollFD := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		millis := int64(remaining / time.Millisecond)
		if remaining%time.Millisecond != 0 {
			millis++
		}
		if millis < 1 {
			millis = 1
		}
		if maxInt := int64(^uint(0) >> 1); millis > maxInt {
			millis = maxInt
		}
		n, err := unix.Poll(pollFD, int(millis))
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return false, fmt.Errorf("poll stdin: %w", err)
		}
		if n > 0 && pollFD[0].Revents != 0 {
			return true, nil
		}
	}
}
