package runtime

import (
	"context"
	"maps"
	"strings"

	"github.com/nexssp/kernel/action"
)

// JSONClean strips the boilerplate that reasoning models habitually
// wrap around structured output: markdown code fences, "Here is the
// JSON" preambles, trailing commentary, and single-line fence forms.
//
// Input:  string, or map with a `content` field carrying a string.
// Output: string (the cleaned JSON body). When input is a map, the
//
//	cleaned body replaces `content` and the map is passed through
//	otherwise unchanged.
var JSONClean = action.New("json.clean", func(_ context.Context, in any) (any, error) {
	raw := ""
	var source map[string]any

	switch v := in.(type) {
	case string:
		raw = v
	case map[string]any:
		if s, ok := v["content"].(string); ok {
			raw = s
			source = v
		}
	}

	if source == nil {
		return cleanJSONString(raw), nil
	}

	out := make(map[string]any, len(source))
	maps.Copy(out, source)
	out["content"] = cleanJSONString(raw)
	return out, nil
}).Description("Strip markdown fences and prose around a JSON body").
	Tag("base", "json").
	Build()

func cleanJSONString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	if after, ok := strings.CutPrefix(s, "```"); ok {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		} else {
			s = after
			s = strings.TrimSuffix(s, "```")
			return strings.TrimSpace(s)
		}
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}

	start := firstTopLevelJSONOpen(s)
	if start < 0 {
		return s
	}
	end := matchingJSONClose(s, start)
	if end < 0 {
		return strings.TrimSpace(s[start:])
	}
	return strings.TrimSpace(s[start:end])
}

func firstTopLevelJSONOpen(s string) int {
	for i := range len(s) {
		c := s[i]
		if c != '{' && c != '[' {
			continue
		}
		if i+1 >= len(s) {
			return -1
		}
		next := s[i+1]
		switch {
		case next == ' ' || next == '\t' || next == '\n' || next == '\r':
			return i
		case next == '"' || next == '{' || next == '[':
			return i
		case next == '-' || (next >= '0' && next <= '9'):
			return i
		case next == 't' || next == 'f' || next == 'n':
			return i
		case next == '}' || next == ']':
			return i
		}
	}
	return -1
}

func matchingJSONClose(s string, openIndex int) int {
	opener := s[openIndex]
	var closer byte
	switch opener {
	case '{':
		closer = '}'
	case '[':
		closer = ']'
	default:
		return -1
	}

	depth := 1
	i := openIndex + 1
	var quote byte

	for i < len(s) {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case opener:
			depth++
		case closer:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		i++
	}
	return -1
}
