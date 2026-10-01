package require

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nexssp/flow/core"
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
		buffer.WriteByte(' ')
	}
	return nil, 0, errors.New("unclosed `{`")
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
		buffer.WriteByte(' ')
	}
	return nil, 0, errors.New("unclosed `{`")
}

// splitOptions parses `key: value, key: value` pairs.
func splitOptions(raw string) (map[string]string, error) {
	out := make(map[string]string)
	for _, pair := range core.SplitTopLevel(raw, ',') {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		colon := core.IndexTopLevel(pair, ':')
		if colon < 0 {
			return nil, fmt.Errorf("expected `key: value`, got %q", pair)
		}
		key := strings.TrimSpace(pair[:colon])
		if key == "" {
			return nil, fmt.Errorf("empty key in %q", pair)
		}
		out[key] = core.TrimQuotes(strings.TrimSpace(pair[colon+1:]))
	}
	return out, nil
}
