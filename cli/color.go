package cli

import (
	"io"
	"os"
	"strings"
)

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
)

func colorEnabled(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func paint(s, color string, enabled bool) string {
	if !enabled || color == "" {
		return s
	}
	return color + s + ansiReset
}

func bold(s string, enabled bool) string { return paint(s, ansiBold, enabled) }
func dim(s string, enabled bool) string  { return paint(s, ansiDim, enabled) }
func cyan(s string, enabled bool) string { return paint(s, ansiCyan, enabled) }
func red(s string, enabled bool) string  { return paint(s, ansiRed, enabled) }

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
