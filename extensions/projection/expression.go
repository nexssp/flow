package projection

import (
	"errors"
	"fmt"
	"strconv"
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
	return buildProgramSourceWithVisibleNames(raw, nil)
}

func buildProgramSourceWithVisibleNames(raw string, visibleNames map[string]struct{}) (string, error) {
	letPrefix, body, bindings, err := splitLeadingProjectionLets(raw)
	if err != nil {
		return "", err
	}
	if err := validateProjectionLetReferences(bindings); err != nil {
		return "", err
	}
	if err := validateProjectionInputSpreadSpacing(body); err != nil {
		return "", err
	}
	localNames := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		localNames[binding.name] = struct{}{}
	}
	if err := validateProjectionObjectKeys("{" + body + "}"); err != nil {
		return "", err
	}

	var object string
	state, rest, hasSpread := splitSpread(body)
	switch {
	case !hasSpread:
		body = rewriteProjectionDataValues(body, localNames, visibleNames)
		object = "{ " + expandProjectionShorthand(body) + " }"
	case strings.TrimSpace(rest) == "":
		object = state
	default:
		rest = rewriteProjectionDataValues(rest, localNames, visibleNames)
		object = "__spread__(" + state + ", { " + expandProjectionShorthand(rest) + " })"
	}
	return compiler.PreprocessDots(letPrefix + object), nil
}

type projectionEntrySpan struct {
	start int
	end   int
}

func rewriteProjectionDataValues(raw string, localNames, visibleNames map[string]struct{}) string {
	entries := splitProjectionEntrySpans(raw)
	var out strings.Builder
	cursor := 0
	for _, span := range entries {
		out.WriteString(raw[cursor:span.start])
		out.WriteString(rewriteProjectionDataEntry(raw[span.start:span.end], localNames, visibleNames))
		cursor = span.end
	}
	out.WriteString(raw[cursor:])
	return out.String()
}

func splitProjectionEntrySpans(raw string) []projectionEntrySpan {
	var spans []projectionEntrySpan
	start := 0
	var quote byte
	delimiters := projectionDelimiters{}
	for i := 0; i < len(raw); {
		c := raw[i]
		if quote != 0 {
			i, quote = advanceProjectionQuote(raw, i, quote)
			continue
		}
		if isProjectionQuote(c) {
			quote = c
			i++
			continue
		}
		if c == '#' && (delimiters.paren == 0 || projectionHashLineComment(raw, i)) {
			i = skipProjectionComment(raw, i)
			continue
		}
		if c == '/' && i+1 < len(raw) {
			switch raw[i+1] {
			case '/':
				i = skipProjectionComment(raw, i)
				continue
			case '*':
				end, ok := projectionBlockCommentEnd(raw, i)
				if ok {
					i = end
					continue
				}
				i = len(raw)
				continue
			}
		}
		if c == ',' && delimiters.empty() {
			spans = append(spans, projectionEntrySpan{start: start, end: i})
			start = i + 1
			i++
			continue
		}
		delimiters.consume(c)
		i++
	}
	return append(spans, projectionEntrySpan{start: start, end: len(raw)})
}

func rewriteProjectionDataEntry(entry string, localNames, visibleNames map[string]struct{}) string {
	start, end, kind, ok := projectionDirectDataValue(entry, localNames, visibleNames)
	if !ok {
		return entry
	}
	switch kind {
	case 's':
		return entry[:start] + strconv.Quote(entry[start+1:end-1]) + entry[end:]
	case 'b':
		return entry[:start] + strconv.Quote(entry[start:end]) + entry[end:]
	case 'o':
		nested := rewriteProjectionDataValues(entry[start+1:end-1], localNames, visibleNames)
		return entry[:start+1] + nested + entry[end-1:]
	default:
		return entry
	}
}

// projectionDirectDataValue finds a whole quoted string, simple bare name/path,
// or nested object after a map-entry colon. Composite expressions and arrays
// remain expr-lang source.
func projectionDirectDataValue(entry string, localNames, visibleNames map[string]struct{}) (start, end int, kind byte, ok bool) {
	colon := projectionMapEntryColon(entry)
	if colon < 0 {
		return 0, 0, 0, false
	}
	start = colon + 1
	for start < len(entry) && isProjectionWhitespace(entry[start]) {
		start++
	}
	if start >= len(entry) {
		return 0, 0, 0, false
	}
	switch entry[start] {
	case '"', '\'':
		end, ok = projectionQuotedLiteralEnd(entry, start)
		kind = 's'
	case '{':
		var err error
		closeIndex, err := scanBalancedBraces(entry, start)
		ok = err == nil
		if ok {
			end = closeIndex + 1
		}
		kind = 'o'
	default:
		end = projectionBareDataNameEnd(entry, start)
		ok = end > start && entry[start:end] != "true" && entry[start:end] != "false" && entry[start:end] != "null" && entry[start:end] != "nil"
		if ok {
			root, _, _ := strings.Cut(entry[start:end], ".")
			_, isLocal := localNames[root]
			_, isVisible := visibleNames[root]
			ok = !isLocal && !isVisible
		}
		kind = 'b'
	}
	if !ok || !projectionTailIsTrivia(entry, end) {
		return 0, 0, 0, false
	}
	return start, end, kind, true
}

func projectionMapEntryColon(entry string) int {
	var quote byte
	delimiters := projectionDelimiters{}
	for i := 0; i < len(entry); {
		c := entry[i]
		if quote != 0 {
			i, quote = advanceProjectionQuote(entry, i, quote)
			continue
		}
		if isProjectionQuote(c) {
			quote = c
			i++
			continue
		}
		if c == '#' && (delimiters.paren == 0 || projectionHashLineComment(entry, i)) {
			return -1
		}
		if c == '/' && i+1 < len(entry) {
			if entry[i+1] == '/' || entry[i+1] == '*' {
				return -1
			}
		}
		if c == ':' && delimiters.empty() {
			return i
		}
		delimiters.consume(c)
		i++
	}
	return -1
}

func projectionQuotedLiteralEnd(raw string, start int) (int, bool) {
	quote := raw[start]
	for i := start + 1; i < len(raw); {
		next, nextQuote := advanceProjectionQuote(raw, i, quote)
		if nextQuote == 0 {
			return next, true
		}
		i = next
	}
	return len(raw), false
}

func projectionBareDataNameEnd(raw string, start int) int {
	if start >= len(raw) || !isProjectionIdentifierStart(raw[start]) {
		return start
	}
	position := start
	for {
		for position < len(raw) && isProjectionIdentifierContinue(raw[position]) {
			position++
		}
		if position+1 >= len(raw) || raw[position] != '.' || !isProjectionIdentifierStart(raw[position+1]) {
			return position
		}
		position += 2
	}
}

func projectionTailIsTrivia(raw string, position int) bool {
	for position < len(raw) {
		if isProjectionWhitespace(raw[position]) {
			position++
			continue
		}
		if raw[position] == '#' || raw[position] == '/' && position+1 < len(raw) && raw[position+1] == '/' {
			position = skipProjectionComment(raw, position)
			continue
		}
		if raw[position] == '/' && position+1 < len(raw) && raw[position+1] == '*' {
			end, ok := projectionBlockCommentEnd(raw, position)
			if !ok {
				return false
			}
			position = end
			continue
		}
		return false
	}
	return true
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
	if name == expressionRootName || name == expressionStateName {
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
	if c == '\\' && quote != '`' && position+1 < len(raw) {
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
	containers      []byte
	lastSignificant byte
	bareFields      *[]projectionIdentifierSpan
}

// expandProjectionShorthand rewrites a bare identifier in an object-entry
// position to an explicit reference to the same input field. It only runs on
// projection bodies; structured argument objects keep their literal grammar.
func expandProjectionShorthand(raw string) string {
	scanner := projectionShorthandScanner{raw: raw, lastSignificant: '{', containers: []byte{'{'}}
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
	case c == '.' && s.isObjectEntryStart():
		return s.copyPathShorthand(index)
	case isProjectionIdentifierStart(c):
		return s.copyIdentifier(index)
	default:
		return s.copyByte(index)
	}
}

func (s *projectionShorthandScanner) copyQuoted(index int) int {
	next, quote := advanceProjectionQuote(s.raw, index, s.quote)
	s.rewritten.WriteString(s.raw[index:next])
	s.quote = quote
	return next
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
		if s.bareFields != nil {
			*s.bareFields = append(*s.bareFields, projectionIdentifierSpan{start: index, end: end})
		}
		s.rewritten.WriteString(": .")
		s.rewritten.WriteString(field)
	}
	s.lastSignificant = s.raw[end-1]
	return end
}

func (s *projectionShorthandScanner) isBareField(end int) bool {
	return s.isObjectEntryStart() && projectionEntryEndsAt(s.raw, end, s.parenDepth)
}

func (s *projectionShorthandScanner) isObjectEntryStart() bool {
	if s.lastSignificant == '{' {
		return true
	}
	return s.lastSignificant == ',' && len(s.containers) > 0 && s.containers[len(s.containers)-1] == '{'
}

func (s *projectionShorthandScanner) copyPathShorthand(index int) int {
	end, key, ok := projectionPathAt(s.raw, index)
	if !ok || !projectionEntryEndsAt(s.raw, end, s.parenDepth) {
		return s.copyByte(index)
	}
	s.rewritten.WriteString(key)
	s.rewritten.WriteString(": ")
	s.rewritten.WriteString(s.raw[index:end])
	s.lastSignificant = s.raw[end-1]
	return end
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
	case '[', '{':
		s.containers = append(s.containers, c)
	case ']':
		if len(s.containers) > 1 && s.containers[len(s.containers)-1] == '[' {
			s.containers = s.containers[:len(s.containers)-1]
		}
	case '}':
		if len(s.containers) > 1 && s.containers[len(s.containers)-1] == '{' {
			s.containers = s.containers[:len(s.containers)-1]
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

func projectionPathAt(raw string, index int) (end int, key string, ok bool) {
	if index >= len(raw) || raw[index] != '.' {
		return index, "", false
	}
	position := index + 1
	for {
		start := position
		if position >= len(raw) || !isProjectionIdentifierStart(raw[position]) {
			return index, "", false
		}
		position++
		for position < len(raw) && (isProjectionIdentifierContinue(raw[position]) || raw[position] == '-') {
			position++
		}
		key = raw[start:position]
		if position >= len(raw) || raw[position] != '.' {
			return position, key, true
		}
		position++
	}
}

func projectionInputPathAt(raw string) (end int, ok bool) {
	const root = "input"
	if len(raw) <= len(root) || !strings.HasPrefix(raw, root) || raw[len(root)] != '.' {
		return 0, false
	}
	end, _, ok = projectionPathAt(raw, len(root))
	return end, ok
}

func projectionSpreadRefEndsAt(raw string, end int) bool {
	return end == len(raw) || isProjectionWhitespace(raw[end]) || raw[end] == ','
}

func validateProjectionInputSpreadSpacing(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "...") {
		return nil
	}
	after := trimmed[3:]
	if after == "" || !isProjectionWhitespace(after[0]) {
		return nil
	}
	after = strings.TrimLeft(after, " \t\n\r")
	end, ok := projectionInputPathAt(after)
	if ok && projectionSpreadRefEndsAt(after, end) {
		return errors.New(`projection: original-input spreads must use "...input.path" with no whitespace after "..."`)
	}
	return nil
}

// validateProjectionObjectKeys rejects duplicates after path shorthand has
// derived their output keys, matching the argument-map duplicate policy.
func validateProjectionObjectKeys(raw string) error {
	scanner := projectionObjectKeyScanner{raw: raw}
	for position := 0; position < len(raw); {
		next, err := scanner.advance(position)
		if err != nil {
			return err
		}
		position = next
	}
	return nil
}

type projectionObjectFrame struct {
	parenDepth   int
	bracketDepth int
	keys         map[string]struct{}
}

type projectionObjectKeyScanner struct {
	raw          string
	frames       []projectionObjectFrame
	quote        byte
	parenDepth   int
	bracketDepth int
}

func (s *projectionObjectKeyScanner) advance(position int) (int, error) {
	current := s.raw[position]
	if s.quote != 0 {
		next, quote := advanceProjectionQuote(s.raw, position, s.quote)
		s.quote = quote
		return next, nil
	}
	if isProjectionQuote(current) {
		s.quote = current
		return position + 1, nil
	}
	if current == '#' && s.parenDepth == 0 {
		return skipProjectionComment(s.raw, position), nil
	}
	switch current {
	case '(':
		s.parenDepth++
	case ')':
		if s.parenDepth > 0 {
			s.parenDepth--
		}
	case '[':
		s.bracketDepth++
	case ']':
		if s.bracketDepth > 0 {
			s.bracketDepth--
		}
	case '{':
		frame := projectionObjectFrame{
			parenDepth:   s.parenDepth,
			bracketDepth: s.bracketDepth,
			keys:         make(map[string]struct{}),
		}
		s.frames = append(s.frames, frame)
		return position + 1, s.addKey(&s.frames[len(s.frames)-1], position+1)
	case '}':
		if len(s.frames) > 0 {
			s.frames = s.frames[:len(s.frames)-1]
		}
	case ',':
		if len(s.frames) > 0 {
			frame := &s.frames[len(s.frames)-1]
			if s.parenDepth == frame.parenDepth && s.bracketDepth == frame.bracketDepth {
				return position + 1, s.addKey(frame, position+1)
			}
		}
	}
	return position + 1, nil
}

func (s *projectionObjectKeyScanner) addKey(frame *projectionObjectFrame, position int) error {
	key, ok := projectionObjectEntryKey(s.raw, position)
	if !ok {
		return nil
	}
	if _, exists := frame.keys[key]; exists {
		return fmt.Errorf("projection: duplicate key %q", key)
	}
	frame.keys[key] = struct{}{}
	return nil
}

func projectionObjectEntryKey(raw string, position int) (string, bool) {
	position = skipProjectionTrivia(raw, position)
	key, position, ok := projectionObjectEntryKeyAt(raw, position)
	if !ok {
		return "", false
	}
	position = skipProjectionTrivia(raw, position)
	if position >= len(raw) {
		return key, true
	}
	switch raw[position] {
	case ':', ',', '}':
		return key, true
	default:
		return "", false
	}
}

func projectionObjectEntryKeyAt(raw string, position int) (key string, next int, ok bool) {
	if position >= len(raw) || strings.HasPrefix(raw[position:], "...") {
		return "", position, false
	}
	switch {
	case raw[position] == '.':
		end, key, ok := projectionPathAt(raw, position)
		if !ok {
			return "", position, false
		}
		return key, end, true
	case isProjectionIdentifierStart(raw[position]):
		start := position
		for position < len(raw) && isProjectionIdentifierContinue(raw[position]) {
			position++
		}
		return raw[start:position], position, true
	default:
		return "", position, false
	}
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
	if end, path := projectionInputPathAt(after); path && projectionSpreadRefEndsAt(after, end) {
		stateRef = after[:end]
		after = after[end:]
	} else {
		if after != "" && after[0] != ',' && after[0] != ' ' && after[0] != '\t' && after[0] != '{' {
			return "", raw, false
		}
		after = strings.TrimLeft(after, " \t\n")
		end, _, path := projectionPathAt(after, 0)
		if path && projectionSpreadRefEndsAt(after, end) {
			stateRef = after[:end]
			after = after[end:]
		} else {
			stateRef = expressionRootName
		}
	}
	after = strings.TrimLeft(after, " \t\n")
	after = strings.TrimPrefix(after, ",")
	after = strings.TrimLeft(after, " \t\n")
	if after != "" && after[0] == '{' {
		if end, err := scanBalancedBraces(after, 0); err == nil && projectionTailIsTrivia(after, end+1) {
			after = strings.TrimSpace(after[1:end])
		}
	}
	return stateRef, after, true
}
