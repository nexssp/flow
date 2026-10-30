package projection

import (
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// buildProgramSource produces the expr-lang source string from the raw
// projection body:
//
//	{ name: .name }           →  "{ name: name }"
//	{ name }                  →  "{ name: name }"
//	{ ..., x: 1 }             →  "__spread__(__root__, { x: 1 })"
//	{ ... }                   →  "__root__"
func buildProgramSource(raw string) string {
	state, rest, hasSpread := splitSpread(raw)
	if !hasSpread {
		return "{ " + compiler.PreprocessDots(expandProjectionShorthand(raw)) + " }"
	}
	if strings.TrimSpace(rest) == "" {
		return state
	}
	return "__spread__(" + state + ", { " + compiler.PreprocessDots(expandProjectionShorthand(rest)) + " })"
}

type projectionShorthandScanner struct {
	raw             string
	rewritten       strings.Builder
	quote           byte
	parenDepth      int
	lastSignificant byte
}

// expandProjectionShorthand rewrites a bare identifier in an object-entry
// position to an explicit reference to the same input field. It only runs on
// projection bodies; structured argument objects keep their literal grammar.
func expandProjectionShorthand(raw string) string {
	scanner := projectionShorthandScanner{raw: raw, lastSignificant: '{'}
	scanner.rewritten.Grow(len(raw))
	for index := 0; index < len(raw); {
		index = scanner.rewriteAt(index)
	}
	return scanner.rewritten.String()
}

func (s *projectionShorthandScanner) rewriteAt(index int) int {
	c := s.raw[index]
	switch {
	case s.quote != 0:
		return s.copyQuoted(index)
	case c == '#' && s.parenDepth == 0:
		return s.copyComment(index)
	case isProjectionQuote(c):
		s.quote = c
		s.rewritten.WriteByte(c)
		s.lastSignificant = c
		return index + 1
	case isProjectionIdentifierStart(c):
		return s.copyIdentifier(index)
	default:
		return s.copyByte(index)
	}
}

func (s *projectionShorthandScanner) copyQuoted(index int) int {
	c := s.raw[index]
	s.rewritten.WriteByte(c)
	if c == '\\' && index+1 < len(s.raw) {
		s.rewritten.WriteByte(s.raw[index+1])
		return index + 2
	}
	if c == s.quote {
		s.quote = 0
	}
	return index + 1
}

func (s *projectionShorthandScanner) copyComment(index int) int {
	for index < len(s.raw) && s.raw[index] != '\n' && s.raw[index] != '\r' {
		s.rewritten.WriteByte(s.raw[index])
		index++
	}
	return index
}

func (s *projectionShorthandScanner) copyIdentifier(index int) int {
	end := index + 1
	for end < len(s.raw) && isProjectionIdentifierContinue(s.raw[end]) {
		end++
	}
	field := s.raw[index:end]
	s.rewritten.WriteString(field)
	if s.isBareField(end) {
		s.rewritten.WriteString(": .")
		s.rewritten.WriteString(field)
	}
	s.lastSignificant = s.raw[end-1]
	return end
}

func (s *projectionShorthandScanner) isBareField(end int) bool {
	startsEntry := s.lastSignificant == '{' || s.lastSignificant == ','
	return startsEntry && projectionEntryEndsAt(s.raw, end, s.parenDepth)
}

func (s *projectionShorthandScanner) copyByte(index int) int {
	c := s.raw[index]
	s.rewritten.WriteByte(c)
	switch c {
	case '(':
		s.parenDepth++
	case ')':
		if s.parenDepth > 0 {
			s.parenDepth--
		}
	}
	if !isProjectionWhitespace(c) {
		s.lastSignificant = c
	}
	return index + 1
}

func isProjectionQuote(c byte) bool {
	return c == '"' || c == '\'' || c == '`'
}

func isProjectionWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func projectionEntryEndsAt(raw string, index, parenDepth int) bool {
	for index < len(raw) {
		c := raw[index]
		if c == '#' && parenDepth == 0 {
			for index < len(raw) && raw[index] != '\n' && raw[index] != '\r' {
				index++
			}
			continue
		}
		if isProjectionWhitespace(c) {
			index++
			continue
		}
		return c == ',' || c == '}'
	}
	return true // EOF is the closing edge of the outer projection object.
}

func isProjectionIdentifierStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isProjectionIdentifierContinue(c byte) bool {
	return isProjectionIdentifierStart(c) || c >= '0' && c <= '9'
}

// splitSpread splits `..., body` into a state reference and the rest.
// Returns hasSpread=false for any content that does not start with a
// standalone spread marker.
func splitSpread(raw string) (stateRef, rest string, hasSpread bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "...") {
		return "", raw, false
	}
	after := trimmed[3:]
	if after != "" && after[0] != ',' && after[0] != ' ' && after[0] != '\t' {
		return "", raw, false
	}
	after = strings.TrimLeft(after, " \t\n")
	after = strings.TrimPrefix(after, ",")
	after = strings.TrimLeft(after, " \t\n")
	return "__root__", after, true
}
