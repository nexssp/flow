package core

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nexssp/flow/compiler"
)

func StripDirectivePrefix(line, name string) (rest string, ok bool) {
	prefix := "@" + name
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	after := line[len(prefix):]
	if after == "" {
		return "", true
	}
	switch after[0] {
	case ' ', '\t', ':':
		return strings.TrimSpace(after[1:]), true
	default:
		return "", false
	}
}

func SplitNameAndAttrs(s string) (string, map[string]string, error) {
	s = strings.TrimSpace(s)

	brace := strings.IndexByte(s, '{')
	if brace < 0 {
		return strings.TrimSpace(s), nil, nil
	}
	name := strings.TrimSpace(s[:brace])
	if name == "" {
		return "", nil, fmt.Errorf("missing name before '{'")
	}
	closeIdx := strings.LastIndexByte(s, '}')
	if closeIdx < brace {
		return "", nil, fmt.Errorf("missing closing '}'")
	}
	body := strings.TrimSpace(s[brace+1 : closeIdx])
	attrs, err := parseAttrBody(body)
	if err != nil {
		return "", nil, err
	}
	return name, attrs, nil
}

func parseAttrBody(s string) (map[string]string, error) {
	out := make(map[string]string)
	if s == "" {
		return out, nil
	}
	for _, pair := range SplitTopLevel(s, ',') {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		colon := strings.IndexByte(pair, ':')
		if colon < 0 {
			return nil, fmt.Errorf("missing ':' in attribute %q", pair)
		}
		key := strings.TrimSpace(pair[:colon])
		val := strings.TrimSpace(pair[colon+1:])
		if key == "" {
			return nil, fmt.Errorf("empty attribute key in %q", pair)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate attribute %q", key)
		}
		out[key] = val
	}
	return out, nil
}

func ParseList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	parts := SplitTopLevel(s, ',')
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = TrimQuotes(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func TrimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') ||
			(s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func ParseFloat(s string) (float64, error) {
	s = TrimQuotes(strings.TrimSpace(s))
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return v, nil
}

func ParseInt(s string) (int, error) {
	s = TrimQuotes(strings.TrimSpace(s))
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("not an integer: %q", s)
	}
	return v, nil
}

func AtErr(ctx *Context, line int, name, msg string) error {
	return compiler.SourceError(
		Position{File: ctx.File, Line: line + 1},
		"@%s: %s", name, msg,
	)
}

func AtErrf(ctx *Context, line int, name, format string, args ...any) error {
	return AtErr(ctx, line, name, fmt.Sprintf(format, args...))
}

// SplitBlock extracts the header and body of a `{ ... }` block.
//
// Two forms are supported:
//
//  1. Multi-line blocks, one entry per line:
//
//     @foo name {
//     key: value,
//     other: value
//     }
//
//  2. Single-line blocks with top-level comma-separated entries:
//
//     @foo name { key: value, other: value }
//
// Both forms produce the same body layout: one entry per element of the
// returned slice. That means a directive can iterate over body uniformly
// and never has to care whether the author wrote the block on one line
// or spread it out.
//
// Every entry is split through SplitTopLevel, so trailing commas (a
// natural style when fields are one-per-line) are stripped consistently,
// and commas inside quoted strings, nested braces, or nested brackets do
// not split entries.
//
// The returned `next` index always points to the first line after the
// block. Single-line blocks advance by one; multi-line blocks advance
// past the closing brace.
func SplitBlock(lines []string, start int) (header string, body []string, next int, err error) {
	if start < 0 || start >= len(lines) {
		return "", nil, start, fmt.Errorf("SplitBlock: start index %d out of range", start)
	}

	first := lines[start]
	openIdx := strings.IndexByte(first, '{')
	if openIdx < 0 {
		return "", nil, start, fmt.Errorf("SplitBlock: no '{' on line %d", start+1)
	}

	header = strings.TrimSpace(first[:openIdx])
	afterOpen := first[openIdx+1:]

	if closeIdx := strings.IndexByte(afterOpen, '}'); closeIdx >= 0 {
		content := strings.TrimSpace(afterOpen[:closeIdx])
		trailing := strings.TrimSpace(afterOpen[closeIdx+1:])
		if trailing != "" &&
			!strings.HasPrefix(trailing, "#") &&
			!strings.HasPrefix(trailing, "//") {
			return "", nil, start,
				fmt.Errorf("SplitBlock: unexpected content after '}': %q", trailing)
		}
		appendEntries(&body, content)
		return header, body, start + 1, nil
	}

	appendEntries(&body, afterOpen)

	for j := start + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "}" {
			return header, body, j + 1, nil
		}
		appendEntries(&body, lines[j])
	}

	return "", nil, start,
		fmt.Errorf("SplitBlock: missing closing '}' for block starting at line %d", start+1)
}

// appendEntries splits s on top-level commas and appends each non-empty,
// trimmed entry to *body. Called for every line of a block; single-line
// blocks pass their whole content once, multi-line blocks call it per
// line. The uniform treatment is what makes trailing commas harmless in
// both forms.
func appendEntries(body *[]string, s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	for _, part := range SplitTopLevel(s, ',') {
		if p := strings.TrimSpace(part); p != "" {
			*body = append(*body, p)
		}
	}
}

func SplitTopLevel(s string, sep byte) []string {
	var (
		out   []string
		start int
		depth int
		inQ   bool
		qc    byte
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQ {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == qc {
				inQ = false
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inQ = true
			qc = c
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
