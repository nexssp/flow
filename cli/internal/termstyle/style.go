// Package termstyle provides the self-test palette for human-readable CLI reports.
package termstyle

import (
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// Style enables ANSI styling only for an interactive output stream.
type Style bool

// New respects redirected output and the NO_COLOR convention.
func New(w io.Writer) Style {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	return Style(ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())))
}

func (s Style) paint(code, text string) string {
	if !s {
		return text
	}
	return code + text + "\x1b[0m"
}

// Bold emphasizes headings and labels.
func (s Style) Bold(text string) string { return s.paint("\x1b[1m", text) }

// Dim de-emphasizes secondary details.
func (s Style) Dim(text string) string { return s.paint("\x1b[2m", text) }

// Red marks failures.
func (s Style) Red(text string) string { return s.paint("\x1b[31m", text) }

// Green marks successful or configured values.
func (s Style) Green(text string) string { return s.paint("\x1b[32m", text) }

// Cyan marks sections.
func (s Style) Cyan(text string) string { return s.paint("\x1b[36m", text) }

// Magenta colors banner rules.
func (s Style) Magenta(text string) string { return s.paint("\x1b[35m", text) }

// Banner mirrors the self-test title, subtitle and rule layout.
func (s Style) Banner(title, subtitle string) string {
	rule := s.Magenta(strings.Repeat("─", 70))
	return "\n" + rule + "\n" + s.Bold("  NEXSS FLOW · "+title) + "\n" + s.Dim("  "+subtitle) + "\n" + rule + "\n\n"
}

// Section introduces a report section using the self-test marker.
func (s Style) Section(title string) string {
	return "\n" + s.Cyan("▸") + " " + s.Bold(title) + "\n\n"
}
