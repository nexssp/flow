package terminal

import (
	"io"
	"os"
)

// IsTerminal reports whether w is a character device such as a terminal.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
