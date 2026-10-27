package runner

import (
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/compiler"
)

const (
	diagnosticANSIRed    = "\x1b[31m"
	diagnosticANSIYellow = "\x1b[33m"
	diagnosticANSIReset  = "\x1b[0m"
)

// FormatErrorDetail removes runner wrapper noise and, when the error carries a
// source position, appends the relevant source line and nearby context. The
// supplied source is preferred for the current file so callers need not reread
// it from disk; included-file positions are read from their own path.
func FormatErrorDetail(err error, source, name string, useColor bool) string {
	if err == nil {
		return ""
	}

	message := err.Error()
	for {
		if index := strings.Index(message, "execution failed: "); index >= 0 {
			message = message[:index] + message[index+len("execution failed: "):]
		} else {
			break
		}
	}
	message = strings.TrimSpace(message)

	position, ok := compiler.PositionOf(err)
	if !ok {
		return message
	}

	text, ok := diagnosticSource(position, source, name)
	if !ok {
		return message
	}
	snippet := renderDiagnosticSnippet(position, text, useColor)
	if snippet == "" {
		return message
	}
	return message + "\n" + snippet
}

func diagnosticSource(position compiler.Position, source, name string) (string, bool) {
	if position.File == "" {
		return "", false
	}
	if position.File == name {
		return source, source != ""
	}
	if strings.HasPrefix(position.File, "<") {
		return "", false
	}
	data, err := os.ReadFile(position.File)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func renderDiagnosticSnippet(position compiler.Position, source string, useColor bool) string {
	lines := strings.Split(source, "\n")
	if position.Line < 1 || position.Line > len(lines) {
		return ""
	}

	target := lines[position.Line-1]
	var builder strings.Builder
	builder.WriteString("\n")

	if position.Line > 1 {
		fmt.Fprintf(&builder, "  %4d │ %s\n", position.Line-1, lines[position.Line-2])
	}
	fmt.Fprintf(&builder, "  %4d │ %s\n", position.Line, target)
	builder.WriteString("       │ ")

	column := position.Col
	if column < 1 {
		column = diagnosticLeadingWhitespace(target) + 1
	}
	for i := 0; i < column-1 && i < len(target); i++ {
		if target[i] == '\t' {
			builder.WriteByte('\t')
		} else {
			builder.WriteByte(' ')
		}
	}
	builder.WriteString(diagnosticPaint("^", diagnosticANSIYellow, useColor))

	if position.Line < len(lines) && strings.TrimSpace(lines[position.Line]) != "" {
		fmt.Fprintf(&builder, "\n  %4d │ %s", position.Line+1, lines[position.Line])
	}
	return builder.String()
}

func diagnosticLeadingWhitespace(line string) int {
	for i := range len(line) {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return 0
}

func diagnosticPaint(text, color string, enabled bool) string {
	if !enabled {
		return text
	}
	return color + text + diagnosticANSIReset
}
