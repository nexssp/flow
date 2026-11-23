//go:build !unix && !windows

package cli

import (
	"fmt"
	"os"
	"time"
)

func waitForFileReadable(*os.File, time.Duration) (bool, error) {
	return false, fmt.Errorf("bounded stdin read is unsupported for this file type on this platform")
}
