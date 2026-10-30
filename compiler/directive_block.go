package compiler

import (
	"errors"
	"fmt"
	"strings"
)

// BlockEntry is one top-level comma-separated block entry and its source span.
type BlockEntry struct {
	Text string
	Span SourceSpan
}

// BlockRead contains the entries and parser-derived spans for a directive block.
type BlockRead struct {
	Body     []string
	Entries  []BlockEntry
	Next     int
	Span     SourceSpan
	BodySpan SourceSpan
}

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
	block, err := ReadBlockWithSpans(lines, start)
	if err != nil {
		return nil, block.Next, err
	}
	return block.Body, block.Next, nil
}

// ReadBlockWithSpans is ReadBlock with source-coordinate spans for each entry,
// the full block and its body. On an unclosed block it returns the partial body
// and a span ending at EOF alongside the same error as ReadBlock.
func ReadBlockWithSpans(lines []string, start int) (BlockRead, error) {
	if start < 0 || start >= len(lines) {
		return BlockRead{Next: start}, errors.New("expected '{'")
	}
	offsets := sourceLineOffsets(lines)
	line := lines[start]
	open := strings.IndexByte(line, '{')
	if open < 0 {
		return BlockRead{Next: start}, errors.New("expected '{'")
	}
	afterOpen := line[open+1:]
	block := BlockRead{Next: start + 1}
	block.Span.Start = offsets[start] + open
	block.BodySpan.Start = offsets[start] + open + 1
	depth := 1
	firstDepth, closeAt, comment := scanBlockLine(afterOpen, depth)
	if closeAt >= 0 {
		content := afterOpen[:closeAt]
		block.Body = splitEntries(content)
		block.Entries = blockEntries(content, offsets[start]+open+1)
		block.Span.End = offsets[start] + open + 1 + closeAt + 1
		block.BodySpan.End = offsets[start] + open + 1 + closeAt
		if rest := strings.TrimSpace(afterOpen[closeAt+1:]); rest != "" && !strings.HasPrefix(rest, "#") && !strings.HasPrefix(rest, "//") {
			return block, fmt.Errorf("unexpected content after '}': %q", rest)
		}
		return block, nil
	}
	depth = firstDepth
	content := afterOpen[:comment]
	block.Body = splitEntries(content)
	block.Entries = blockEntries(content, offsets[start]+open+1)

	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		newDepth, closeAt, comment := scanBlockLine(line, depth)
		if closeAt >= 0 {
			bodyPart := line[:closeAt]
			if strings.TrimSpace(bodyPart) != "" {
				block.Body = append(block.Body, splitEntries(strings.TrimSpace(bodyPart))...)
				block.Entries = append(block.Entries, blockEntries(bodyPart, offsets[i])...)
			}
			rest := strings.TrimSpace(line[closeAt+1:])
			if rest != "" && !strings.HasPrefix(rest, "#") && !strings.HasPrefix(rest, "//") {
				return block, fmt.Errorf("unexpected content after '}': %q", rest)
			}
			block.Next = i + 1
			block.Span.End = offsets[i] + closeAt + 1
			block.BodySpan.End = offsets[i] + closeAt
			return block, nil
		}
		depth = newDepth
		bodyPart := line[:comment]
		if strings.TrimSpace(bodyPart) == "" {
			continue
		}
		block.Body = append(block.Body, splitEntries(strings.TrimSpace(bodyPart))...)
		block.Entries = append(block.Entries, blockEntries(bodyPart, offsets[i])...)
	}
	block.Next = 0
	block.Span.End = offsets[len(lines)-1] + len(lines[len(lines)-1])
	block.BodySpan.End = block.Span.End
	return block, errors.New("unclosed '{'")
}

func scanBlockLine(line string, depth int) (nextDepth, closeAt, comment int) {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			continue
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			return depth, -1, i
		}
		if c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t' || line[i-1] == '\r') {
			return depth, -1, i
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return depth, i, len(line)
			}
		}
	}
	return depth, -1, len(line)
}

func blockEntries(content string, base int) []BlockEntry {
	parts := SplitTopLevel(content, ',')
	entries := make([]BlockEntry, 0, len(parts))
	searchFrom := 0
	for _, part := range parts {
		start := searchFrom
		end := start + len(part)
		searchFrom = end + 1
		leading := len(part) - len(strings.TrimLeft(part, " \t\r\n"))
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		entries = append(entries, BlockEntry{
			Text: text,
			Span: SourceSpan{Start: base + start + leading, End: base + start + leading + len(text)},
		})
	}
	return entries
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
