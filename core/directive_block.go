package core

import (
	"errors"
	"fmt"
	"strings"
)

// ReadBlock extracts the raw body of a `{ ... }` block starting at
// lines[start]. Handles all three shapes uniformly:
//
//	inline:      @x NAME { a: 1, b: 2 }
//	multi-line:  @x NAME {
//	               a: 1
//	               b: 2
//	             }
//	tail-brace:  @x NAME {
//	               a: 1
//	               b: 2 }
//
// Returns one entry per top-level comma-separated element. Callers
// interpret entries: `key: value` for @config/@llm/@sandbox/@pool,
// `Field Type` for @schema.
//
// Comments (`#`, `//`) and blank lines inside the block are dropped.
func ReadBlock(lines []string, start int) (body []string, next int, err error) {
	line := lines[start]
	_, after, ok := strings.Cut(line, "{")
	if !ok {
		return nil, start, errors.New("expected '{'")
	}

	afterOpen := after

	// Inline: closing brace on the same line.
	if closeIndex := strings.LastIndexByte(afterOpen, '}'); closeIndex >= 0 {
		return splitEntries(afterOpen[:closeIndex]), start + 1, nil
	}

	body = splitEntries(afterOpen)

	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "}" {
			return body, i + 1, nil
		}
		if strings.HasPrefix(trimmed, "}") {
			if rest := strings.TrimSpace(trimmed[1:]); rest != "" {
				return nil, 0, fmt.Errorf("unexpected content after '}': %q", rest)
			}
			return body, i + 1, nil
		}
		if before, ok := strings.CutSuffix(trimmed, "}"); ok {
			if t := strings.TrimSpace(before); t != "" {
				body = append(body, splitEntries(t)...)
			}
			return body, i + 1, nil
		}
		body = append(body, splitEntries(trimmed)...)
	}
	return nil, 0, errors.New("unclosed '{'")
}

// splitEntries splits s on top-level commas and trims each segment.
// Empty segments are dropped so `a: 1,` and `a: 1` produce the same
// slice.
func splitEntries(s string) []string {
	parts := SplitTopLevel(s, ',')
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ParseBlockOptions reads a `{ key: value, ... }` block via ReadBlock
// and parses each entry as a `key: value` pair. Returns nil for an
// empty block.
func ParseBlockOptions(lines []string, start int) (opts map[string]string, next int, err error) {
	entries, next, err := ReadBlock(lines, start)
	if err != nil {
		return nil, start, err
	}
	if len(entries) == 0 {
		return nil, next, nil
	}

	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := splitOptionLine(entry)
		if !ok {
			return nil, 0, fmt.Errorf("expected `key: value`, got %q", entry)
		}
		out[key] = value
	}
	return out, next, nil
}

// ParseList extracts the first bracketed list `[a, b, c]` from a line.
// Returns nil for `[]` or when no brackets are present.
func ParseList(line string) []string {
	openIndex := strings.IndexByte(line, '[')
	if openIndex < 0 {
		return nil
	}
	closeIndex := strings.IndexByte(line[openIndex+1:], ']')
	if closeIndex < 0 {
		return nil
	}
	body := line[openIndex+1 : openIndex+1+closeIndex]

	var items []string
	for part := range strings.SplitSeq(body, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func splitOptionLine(line string) (key, value string, ok bool) {
	line = strings.TrimSuffix(strings.TrimSpace(line), ",")
	colonIndex := strings.IndexByte(line, ':')
	if colonIndex <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:colonIndex])
	value = TrimQuotes(strings.TrimSpace(line[colonIndex+1:]))
	return key, value, key != ""
}

// SplitTopLevel splits s on sep, respecting quoted strings (" ' `),
// balanced brackets, and backslash escapes. Returns the raw segments,
// not trimmed. Callers trim themselves.
func SplitTopLevel(s string, sep byte) []string {
	var (
		out   []string
		start int
		depth int
		inQ   byte
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQ != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQ {
				inQ = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inQ = c
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
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

// IndexTopLevel returns the index of the first occurrence of target in
// s at bracket depth zero, outside quoted strings. Returns -1 when
// absent. Companion to SplitTopLevel.
func IndexTopLevel(s string, target byte) int {
	var (
		depth int
		inQ   byte
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQ != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQ {
				inQ = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inQ = c
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			depth--
		case target:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// TrimQuotes removes one matched pair of surrounding quotes (" ' `),
// or returns s unchanged.
func TrimQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if first != last {
		return s
	}
	switch first {
	case '"', '\'', '`':
		return s[1 : len(s)-1]
	}
	return s
}
