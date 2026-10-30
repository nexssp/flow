package projection

import (
	"errors"
	"fmt"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// projectionLocalBinding is one declaration at the beginning of a projection.
type projectionLocalBinding struct {
	name  string
	value string
}

// buildProgramSource produces expr-lang source from a projection body. Local
// declarations remain in the same expression so expr-lang scopes them to this
// projection only.
func buildProgramSource(raw string) (string, error) {
	letPrefix, body, bindings, err := splitLeadingProjectionLets(raw)
	if err != nil {
		return "", err
	}
	if err := validateProjectionLetReferences(bindings); err != nil {
		return "", err
	}

	var object string
	state, rest, hasSpread := splitSpread(body)
	switch {
	case !hasSpread:
		object = "{ " + expandProjectionShorthand(body) + " }"
	case strings.TrimSpace(rest) == "":
		object = state
	default:
		object = "__spread__(" + state + ", { " + expandProjectionShorthand(rest) + " })"
	}
	return compiler.PreprocessDots(letPrefix + object), nil
}

// splitLeadingProjectionLets accepts the grammar `let name = expression;`
// repeated zero or more times before the comma-separated projection fields or
// leading spread entry. Names are ordered: initializers may use earlier local
// bindings, but not later ones. Each initializer ends at a top-level semicolon;
// nested delimiters and quoted strings do not terminate it.
func splitLeadingProjectionLets(raw string) (prefix, body string, bindings []projectionLocalBinding, err error) {
	position := skipProjectionTrivia(raw, 0)
	if !startsProjectionLet(raw, position) {
		return "", raw, nil, nil
	}

	seen := make(map[string]struct{})
	for startsProjectionLet(raw, position) {
		binding, semicolon, parseErr := parseProjectionLocalBinding(raw, position)
		if parseErr != nil {
			return "", "", nil, parseErr
		}
		if _, exists := seen[binding.name]; exists {
			return "", "", nil, fmt.Errorf("projection: duplicate local binding %q", binding.name)
		}
		seen[binding.name] = struct{}{}
		bindings = append(bindings, binding)
		position = skipProjectionTrivia(raw, semicolon+1)
	}

	return raw[:position], raw[position:], bindings, nil
}

func parseProjectionLocalBinding(raw string, position int) (projectionLocalBinding, int, error) {
	position += len("let")
	name, position, err := projectionLocalName(raw, position)
	if err != nil {
		return projectionLocalBinding{}, -1, err
	}
	semicolon, err := projectionLetTerminator(raw, position)
	if err != nil {
		return projectionLocalBinding{}, -1, err
	}
	value := strings.TrimSpace(raw[position:semicolon])
	if value == "" {
		return projectionLocalBinding{}, -1, fmt.Errorf("projection: local binding %q requires a value", name)
	}
	return projectionLocalBinding{name: name, value: value}, semicolon, nil
}

func projectionLocalName(raw string, position int) (name string, next int, err error) {
	if position >= len(raw) || !isProjectionWhitespace(raw[position]) {
		return "", -1, errors.New("projection: expected whitespace after let")
	}
	for position < len(raw) && isProjectionWhitespace(raw[position]) {
		position++
	}
	if position >= len(raw) || !isProjectionIdentifierStart(raw[position]) {
		return "", -1, errors.New("projection: let requires a local identifier")
	}
	nameStart := position
	for position < len(raw) && isProjectionIdentifierContinue(raw[position]) {
		position++
	}
	name = raw[nameStart:position]
	if name == "__root__" || name == "__state__" {
		return "", -1, fmt.Errorf("projection: %q is a reserved local name", name)
	}
	next, err = projectionAssignmentStart(raw, name, position)
	return name, next, err
}

func projectionAssignmentStart(raw, name string, position int) (int, error) {
	if position >= len(raw) || !isProjectionWhitespace(raw[position]) {
		return -1, fmt.Errorf("projection: expected '=' after local name %q", name)
	}
	for position < len(raw) && isProjectionWhitespace(raw[position]) {
		position++
	}
	if position >= len(raw) || raw[position] != '=' || position+1 < len(raw) && raw[position+1] == '=' {
		return -1, fmt.Errorf("projection: expected '=' after local name %q", name)
	}
	return position + 1, nil
}

func startsProjectionLet(raw string, position int) bool {
	if position < 0 || position+len("let") > len(raw) || raw[position:position+len("let")] != "let" {
		return false
	}
	if position+len("let") == len(raw) {
		return false
	}
	return isProjectionWhitespace(raw[position+len("let")])
}

func skipProjectionTrivia(raw string, position int) int {
	for position < len(raw) {
		if isProjectionWhitespace(raw[position]) {
			position++
			continue
		}
		if raw[position] == '#' {
			position = skipProjectionComment(raw, position)
			continue
		}
		break
	}
	return position
}

func skipProjectionComment(raw string, position int) int {
	for position < len(raw) && raw[position] != '\n' && raw[position] != '\r' {
		position++
	}
	return position
}

type projectionDelimiters struct {
	paren   int
	bracket int
	brace   int
}

func (d *projectionDelimiters) empty() bool {
	return d.paren == 0 && d.bracket == 0 && d.brace == 0
}

func (d *projectionDelimiters) consume(c byte) bool {
	switch c {
	case '(':
		d.paren++
	case ')':
		if d.paren > 0 {
			d.paren--
		}
	case '[':
		d.bracket++
	case ']':
		if d.bracket > 0 {
			d.bracket--
		}
	case '{':
		d.brace++
	case '}':
		if d.brace == 0 {
			return false
		}
		d.brace--
	}
	return true
}

func projectionLetTerminator(raw string, position int) (int, error) {
	var quote byte
	delimiters := projectionDelimiters{}
	for position < len(raw) {
		c := raw[position]
		if quote != 0 {
			position, quote = advanceProjectionQuote(raw, position, quote)
			continue
		}
		switch {
		case isProjectionQuote(c):
			quote = c
		case c == '#' && delimiters.paren == 0:
			position = skipProjectionComment(raw, position)
			continue
		case c == ';' && delimiters.empty():
			return position, nil
		case !delimiters.consume(c):
			return -1, errors.New("projection: local binding is missing its terminating ';'")
		}
		position++
	}
	return -1, errors.New("projection: local binding is missing its terminating ';'")
}

func advanceProjectionQuote(raw string, position int, quote byte) (next int, nextQuote byte) {
	c := raw[position]
	if c == '\\' && position+1 < len(raw) {
		return position + 2, quote
	}
	if c == quote {
		quote = 0
	}
	return position + 1, quote
}

func validateProjectionLetReferences(bindings []projectionLocalBinding) error {
	for index, binding := range bindings {
		for _, later := range bindings[index+1:] {
			if projectionExpressionReferences(binding.value, later.name) {
				return fmt.Errorf("projection: local binding %q references later binding %q", binding.name, later.name)
			}
		}
	}
	return nil
}

// projectionExpressionReferences recognizes bare variable references while
// ignoring strings, comments, object keys, and member names.
func projectionExpressionReferences(raw, name string) bool {
	scanner := projectionReferenceScanner{name: name}
	for position := 0; position < len(raw); {
		var found bool
		position, found = scanner.advance(raw, position)
		if found {
			return true
		}
	}
	return false
}

type projectionReferenceScanner struct {
	name       string
	quote      byte
	parenDepth int
}

func (s *projectionReferenceScanner) advance(raw string, position int) (int, bool) {
	c := raw[position]
	switch {
	case s.quote != 0:
		next, quote := advanceProjectionQuote(raw, position, s.quote)
		s.quote = quote
		return next, false
	case isProjectionQuote(c):
		s.quote = c
		return position + 1, false
	case c == '#' && s.parenDepth == 0:
		return skipProjectionComment(raw, position), false
	case c == '(':
		s.parenDepth++
		return position + 1, false
	case c == ')':
		if s.parenDepth > 0 {
			s.parenDepth--
		}
		return position + 1, false
	case !isProjectionIdentifierStart(c):
		return position + 1, false
	}
	end := position + 1
	for end < len(raw) && isProjectionIdentifierContinue(raw[end]) {
		end++
	}
	if raw[position:end] != s.name {
		return end, false
	}
	previous := previousProjectionSignificant(raw, position)
	next := nextProjectionSignificant(raw, end)
	return end, previous != '.' && previous != '?' && next != ':'
}

func previousProjectionSignificant(raw string, position int) byte {
	for position--; position >= 0; position-- {
		if !isProjectionWhitespace(raw[position]) {
			return raw[position]
		}
	}
	return 0
}

func nextProjectionSignificant(raw string, position int) byte {
	for position < len(raw) {
		if !isProjectionWhitespace(raw[position]) {
			return raw[position]
		}
		position++
	}
	return 0
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
