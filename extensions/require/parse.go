package require

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

// parseInlineOptions reads a `{ ... }` block whose opening brace is
// inside a line that already carries the module spec.
func parseInlineOptions(inline string, lines []string, startLine int) (opts map[string]string, next int, err error) {
	if closeIndex := strings.IndexByte(inline, '}'); closeIndex >= 0 {
		options, err := splitOptions(inline[1:closeIndex])
		return options, startLine + 1, err
	}

	var buffer strings.Builder
	buffer.WriteString(inline[1:])
	buffer.WriteByte(' ')

	for i := startLine + 1; i < len(lines); i++ {
		if closeIndex := strings.IndexByte(lines[i], '}'); closeIndex >= 0 {
			buffer.WriteString(lines[i][:closeIndex])
			options, err := splitOptions(buffer.String())
			return options, i + 1, err
		}
		buffer.WriteString(lines[i])
		buffer.WriteByte('\n')
	}
	return nil, 0, xerr.Validation("unclosed `{`")
}

// parseBlockOptions reads a `{ ... }` block that starts on its own line.
func parseBlockOptions(lines []string, start int) (opts map[string]string, next int, err error) {
	var buffer strings.Builder
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if i == start {
			if openIndex := strings.IndexByte(line, '{'); openIndex >= 0 {
				line = line[openIndex+1:]
			}
		}
		if closeIndex := strings.IndexByte(line, '}'); closeIndex >= 0 {
			buffer.WriteString(line[:closeIndex])
			options, err := splitOptions(buffer.String())
			return options, i + 1, err
		}
		buffer.WriteString(line)
		buffer.WriteByte('\n')
	}
	return nil, 0, xerr.Validation("unclosed `{`")
}

// splitOptions parses `key: value, key: value` pairs.
func splitOptions(raw string) (map[string]string, error) {
	out := make(map[string]string)
	for _, commaPart := range compiler.SplitTopLevel(raw, ',') {
		for _, pair := range compiler.SplitTopLevel(commaPart, '\n') {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			colon := compiler.IndexTopLevel(pair, ':')
			if colon < 0 {
				return nil, xerr.Validation(fmt.Sprintf("expected `key: value`, got %q", pair))
			}
			key := strings.TrimSpace(pair[:colon])
			if key == "" {
				return nil, xerr.Validation(fmt.Sprintf("empty key in %q", pair))
			}
			out[key] = compiler.TrimQuotes(strings.TrimSpace(pair[colon+1:]))
		}
	}
	return out, nil
}
