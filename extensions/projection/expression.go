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

func buildProgramSourceWithVisibleNames(raw string, visibleNames map[string]struct{}, typedInputNames ...map[string]struct{}) (string, error) {
	var typedNames map[string]struct{}
	if len(typedInputNames) > 0 {
		typedNames = typedInputNames[0]
	}
	letPrefix, body, bindings, err := splitLeadingProjectionLets(raw)
	if err != nil {
		return "", err
	}
	if referenceErr := validateProjectionLetReferences(bindings); referenceErr != nil {
		return "", referenceErr
	}
	if spacingErr := validateProjectionInputSpreadSpacing(body); spacingErr != nil {
		return "", spacingErr
	}
	localNames := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if _, shadowsTypedInput := typedNames[binding.name]; shadowsTypedInput {
			return "", fmt.Errorf("projection: local binding %q shadows a typed pipeline input", binding.name)
		}
		localNames[binding.name] = struct{}{}
	}

	object, _, _, err := buildProjectionObjectBody(body, localNames, visibleNames, typedNames)
	if err != nil {
		return "", err
	}
	return compiler.PreprocessDotsWithRoots(letPrefix+object, typedNames), nil
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

func validateProjectionBareValuesInScope(raw string, visibleNames map[string]struct{}) error {
	_, body, bindings, err := splitLeadingProjectionLets(raw)
	if err != nil {
		return err
	}
	localNames := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		localNames[binding.name] = struct{}{}
	}
	entries, err := projectionNonSpreadEntries(body)
	if err != nil {
		return err
	}
	return validateProjectionBareValues(strings.Join(entries, ", "), localNames, visibleNames)
}

func rejectUnavailableProjectionBindings(raw string, unavailableNames []string, environment map[string]any) error {
	if len(unavailableNames) == 0 {
		return nil
	}
	unavailable := make(map[string]struct{}, len(unavailableNames))
	for _, name := range unavailableNames {
		if _, available := environment[name]; !available {
			unavailable[name] = struct{}{}
		}
	}
	if len(unavailable) == 0 {
		return nil
	}
	_, body, bindings, err := splitLeadingProjectionLets(raw)
	if err != nil {
		return err
	}
	localNames := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		localNames[binding.name] = struct{}{}
	}
	for _, binding := range bindings {
		if unavailableErr := rejectUnavailableProjectionValues(binding.value, localNames, unavailable); unavailableErr != nil {
			return unavailableErr
		}
	}
	entries, err := projectionNonSpreadEntries(body)
	if err != nil {
		return err
	}
	return rejectUnavailableProjectionValues(strings.Join(entries, ", "), localNames, unavailable)
}

func rejectUnavailableProjectionValues(raw string, localNames, unavailable map[string]struct{}) error {
	for _, span := range splitProjectionEntrySpans(raw) {
		entry := raw[span.start:span.end]
		start, end, kind, ok := projectionDirectDataValue(entry, localNames, nil)
		if !ok {
			continue
		}
		switch kind {
		case 'b':
			name := entry[start:end]
			root, _, _ := strings.Cut(name, ".")
			if _, missing := unavailable[root]; missing {
				return fmt.Errorf("pipeline binding %q is unavailable because its action failed", root)
			}
		case 'o':
			if err := rejectUnavailableProjectionValues(entry[start+1:end-1], localNames, unavailable); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProjectionBareValues(raw string, localNames, visibleNames map[string]struct{}) error {
	for _, span := range splitProjectionEntrySpans(raw) {
		entry := raw[span.start:span.end]
		start, end, kind, ok := projectionDirectDataValue(entry, nil, nil)
		if !ok {
			continue
		}
		switch kind {
		case 'b':
			name := entry[start:end]
			root, _, _ := strings.Cut(name, ".")
			if _, local := localNames[root]; local {
				continue
			}
			if _, visible := visibleNames[root]; !visible {
				return fmt.Errorf("unresolved projection value %q; quote literal text or define the name", name)
			}
		case 'o':
			if err := validateProjectionBareValues(entry[start+1:end-1], localNames, visibleNames); err != nil {
				return err
			}
		}
	}
	return nil
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

type projectionSourceEntry struct {
	raw    string
	source string
	fields string
	spread bool
}

// buildProjectionObjectBody lowers each spread and explicit field in source
// order. A later map overrides keys already accumulated, matching ordinary
// object-spread behavior; repeated explicit keys remain compile-time errors.
func buildProjectionObjectBody(raw string, localNames, visibleNames, typedNames map[string]struct{}) (generatedSource string, spreadFound bool, explicitEntries []string, resultErr error) {
	spans := splitProjectionEntrySpans(raw)
	entries := make([]projectionSourceEntry, 0, len(spans))
	topLevelSpread := false
	for _, span := range spans {
		entry := raw[span.start:span.end]
		if projectionEntryIsTrivia(entry) {
			continue
		}
		source, fields, spread, err := parseProjectionSpreadEntry(entry)
		if err != nil {
			return "", false, nil, err
		}
		entries = append(entries, projectionSourceEntry{raw: entry, source: source, fields: fields, spread: spread})
		topLevelSpread = topLevelSpread || spread
	}

	if !topLevelSpread {
		nested, hasSpread, err := rewriteNestedProjectionSpreads(raw, localNames, visibleNames, typedNames)
		if err != nil {
			return "", false, nil, err
		}
		body := rewriteProjectionDataValues(nested, localNames, visibleNames)
		object := "{ " + expandProjectionShorthand(body, typedNames) + " }"
		if err := validateProjectionObjectKeys(object); err != nil {
			return "", false, nil, err
		}
		return object, hasSpread, projectionObjectEntries(body), nil
	}

	var parts []string
	var pendingFields []string
	var explicitFields []string
	flushFields := func() {
		if len(pendingFields) == 0 {
			return
		}
		parts = append(parts, "{ "+strings.Join(pendingFields, ", ")+" }")
		pendingFields = nil
	}

	for _, entry := range entries {
		if !entry.spread {
			nested, _, err := rewriteNestedProjectionSpreads(entry.raw, localNames, visibleNames, typedNames)
			if err != nil {
				return "", false, nil, err
			}
			field := strings.TrimSpace(expandProjectionShorthand(rewriteProjectionDataValues(nested, localNames, visibleNames), typedNames))
			pendingFields = append(pendingFields, field)
			explicitFields = append(explicitFields, field)
			continue
		}

		flushFields()
		parts = append(parts, entry.source)
		if projectionEntryIsTrivia(entry.fields) {
			continue
		}
		fieldObject, _, fields, err := buildProjectionObjectBody(entry.fields, localNames, visibleNames, typedNames)
		if err != nil {
			return "", false, nil, err
		}
		parts = append(parts, fieldObject)
		explicitFields = append(explicitFields, fields...)
	}
	flushFields()

	if len(explicitFields) > 0 {
		if err := validateProjectionObjectKeys("{ " + strings.Join(explicitFields, ", ") + " }"); err != nil {
			return "", false, nil, err
		}
	}
	if len(parts) == 0 {
		return "{  }", false, explicitFields, nil
	}
	object := parts[0]
	for _, part := range parts[1:] {
		object = "__spread__(" + object + ", " + part + ")"
	}
	return object, true, explicitFields, nil
}

func projectionObjectEntries(body string) []string {
	spans := splitProjectionEntrySpans(body)
	entries := make([]string, 0, len(spans))
	for _, span := range spans {
		entry := body[span.start:span.end]
		if !projectionEntryIsTrivia(entry) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func projectionEntryIsTrivia(raw string) bool {
	return projectionTailIsTrivia(raw, 0)
}

// projectionNonSpreadEntries returns fields that need ordinary projection
// validation, preserving source order while omitting spread sources.
func projectionNonSpreadEntries(raw string) ([]string, error) {
	var entries []string
	for _, span := range splitProjectionEntrySpans(raw) {
		entry := raw[span.start:span.end]
		if projectionEntryIsTrivia(entry) {
			continue
		}
		_, fields, spread, err := parseProjectionSpreadEntry(entry)
		if err != nil {
			return nil, err
		}
		if spread {
			if !projectionEntryIsTrivia(fields) {
				entries = append(entries, fields)
			}
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// rewriteNestedProjectionSpreads replaces only nested object literals that
// actually contain a spread, leaving ordinary object source untouched.
func rewriteNestedProjectionSpreads(raw string, localNames, visibleNames, typedNames map[string]struct{}) (rewrittenSource string, containsSpread bool, resultErr error) {
	var out strings.Builder
	out.Grow(len(raw))
	var quote byte
	parenDepth := 0
	changed := false
	for index := 0; index < len(raw); {
		if quote != 0 {
			next, nextQuote := advanceProjectionQuote(raw, index, quote)
			out.WriteString(raw[index:next])
			index, quote = next, nextQuote
			continue
		}
		if next, handled, err := skipBalancedBraceTrivia(raw, index, parenDepth); handled {
			if err != nil {
				return "", false, err
			}
			out.WriteString(raw[index:next])
			index = next
			continue
		}
		switch raw[index] {
		case '"', '\'', '`':
			quote = raw[index]
			out.WriteByte(raw[index])
			index++
		case '(':
			parenDepth++
			out.WriteByte(raw[index])
			index++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
			out.WriteByte(raw[index])
			index++
		case '{':
			closeIndex, err := scanBalancedBraces(raw, index)
			if err != nil {
				return "", false, err
			}
			if projectionFlowBlockBefore(raw, index) {
				body, hasSpread, err := rewriteNestedProjectionSpreads(raw[index+1:closeIndex], localNames, visibleNames, typedNames)
				if err != nil {
					return "", false, err
				}
				if hasSpread {
					out.WriteByte('{')
					out.WriteString(body)
					out.WriteByte('}')
					changed = true
				} else {
					out.WriteString(raw[index : closeIndex+1])
				}
			} else {
				object, hasSpread, _, err := buildProjectionObjectBody(raw[index+1:closeIndex], localNames, visibleNames, typedNames)
				if err != nil {
					return "", false, err
				}
				if hasSpread {
					out.WriteString(object)
					changed = true
				} else {
					out.WriteString(raw[index : closeIndex+1])
				}
			}
			index = closeIndex + 1
		default:
			out.WriteByte(raw[index])
			index++
		}
	}
	if !changed {
		return raw, false, nil
	}
	return out.String(), true, nil
}

// projectionFlowBlockBefore keeps braces owned by Flow keywords as blocks,
// while still allowing nested object literals inside those blocks to rewrite.
func projectionFlowBlockBefore(raw string, brace int) bool {
	position := brace
	for position > 0 && isProjectionWhitespace(raw[position-1]) {
		position--
	}
	if position == 0 {
		return false
	}

	if isProjectionIdentifierContinue(raw[position-1]) {
		return projectionFlowBlockIdentifierBefore(raw, position)
	}
	if raw[position-1] != ')' {
		return false
	}

	var parenHeads []string
	var quote byte
	for index := 0; index < position; {
		if quote != 0 {
			index, quote = advanceProjectionQuote(raw, index, quote)
			continue
		}
		if isProjectionQuote(raw[index]) {
			quote = raw[index]
			index++
			continue
		}
		if next, handled, err := skipBalancedBraceTrivia(raw, index, len(parenHeads)); handled {
			if err != nil {
				return false
			}
			index = next
			continue
		}
		switch raw[index] {
		case '(':
			parenHeads = append(parenHeads, projectionWordBefore(raw, index))
		case ')':
			if len(parenHeads) > 0 {
				head := parenHeads[len(parenHeads)-1]
				parenHeads = parenHeads[:len(parenHeads)-1]
				if index == position-1 {
					return head == "if" || head == "match" || head == "each"
				}
			}
		}
		index++
	}
	return false
}

func projectionFlowBlockIdentifierBefore(raw string, position int) bool {
	start := position - 1
	for start > 0 && isProjectionIdentifierContinue(raw[start-1]) {
		start--
	}
	if start > 0 && raw[start-1] == '.' {
		return false
	}
	switch raw[start:position] {
	case "race", "parallel", "match", "each", "else":
		return true
	default:
		return false
	}
}

func projectionWordBefore(raw string, position int) string {
	for position > 0 && isProjectionWhitespace(raw[position-1]) {
		position--
	}
	end := position
	for position > 0 && isProjectionIdentifierContinue(raw[position-1]) {
		position--
	}
	return raw[position:end]
}

func rewriteProjectionDataEntry(entry string, localNames, visibleNames map[string]struct{}) string {
	start, end, kind, ok := projectionDirectDataValue(entry, localNames, visibleNames)
	if !ok {
		return entry
	}
	switch kind {
	case 's':
		if entry[start] == '"' {
			return entry
		}
		return entry[:start] + strconv.Quote(entry[start+1:end-1]) + entry[end:]
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
	raw                    string
	rewritten              strings.Builder
	emit                   bool // false when callers only need spans or reachability state
	quote                  byte
	parenDepth             int
	containers             []byte
	lastSignificant        byte
	bareFields             *[]projectionIdentifierSpan
	typedInputs            map[string]struct{}
	potentialTypedRootPath *bool
}

func (s *projectionShorthandScanner) writeString(text string) {
	if s.emit {
		s.rewritten.WriteString(text)
	}
}

func (s *projectionShorthandScanner) writeByte(value byte) {
	if s.emit {
		s.rewritten.WriteByte(value)
	}
}

// expandProjectionShorthand rewrites a bare identifier in an object-entry
// position to an explicit reference to the same input field. It only runs on
// projection bodies; structured argument objects keep their literal grammar.
func expandProjectionShorthand(raw string, typedInputNames ...map[string]struct{}) string {
	var typedInputs map[string]struct{}
	if len(typedInputNames) > 0 {
		typedInputs = typedInputNames[0]
	}
	scanner := projectionShorthandScanner{raw: raw, emit: true, lastSignificant: '{', containers: []byte{'{'}, typedInputs: typedInputs}
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
		s.writeByte(c)
		s.lastSignificant = c
		return index + 1
	case c == '.' && s.isObjectEntryStart():
		return s.copyPathShorthand(index)
	case isProjectionIdentifierStart(c):
		if s.isObjectEntryStart() {
			if s.potentialTypedRootPath != nil {
				if _, _, root, ok := projectionAnyNamedPathAt(s.raw, index); ok && root != expressionInputName {
					*s.potentialTypedRootPath = true
				}
			}
			if end, key, ok := projectionNamedPathAt(s.raw, index, s.typedInputs); ok && projectionEntryEndsAt(s.raw, end, s.parenDepth) {
				s.writeString(key)
				s.writeString(": ")
				s.writeString(s.raw[index:end])
				s.lastSignificant = s.raw[end-1]
				return end
			}
		}
		return s.copyIdentifier(index)
	default:
		return s.copyByte(index)
	}
}

func (s *projectionShorthandScanner) copyQuoted(index int) int {
	next, quote := advanceProjectionQuote(s.raw, index, s.quote)
	s.writeString(s.raw[index:next])
	s.quote = quote
	return next
}

func (s *projectionShorthandScanner) copyComment(index int) int {
	for index < len(s.raw) && s.raw[index] != '\n' && s.raw[index] != '\r' {
		s.writeByte(s.raw[index])
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
	s.writeString(field)
	if s.isBareField(end) {
		if s.bareFields != nil {
			*s.bareFields = append(*s.bareFields, projectionIdentifierSpan{start: index, end: end})
		}
		s.writeString(": .")
		s.writeString(field)
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
	s.writeString(key)
	s.writeString(": ")
	s.writeString(s.raw[index:end])
	s.lastSignificant = s.raw[end-1]
	return end
}

func (s *projectionShorthandScanner) copyByte(index int) int {
	c := s.raw[index]
	s.writeByte(c)
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

func projectionNamedPathAt(raw string, index int, typedInputNames map[string]struct{}) (end int, key string, ok bool) {
	end, key, root, ok := projectionAnyNamedPathAt(raw, index)
	if !ok {
		return index, "", false
	}
	_, isTypedInput := typedInputNames[root]
	if root != "input" && !isTypedInput {
		return index, "", false
	}
	return end, key, true
}

func projectionAnyNamedPathAt(raw string, index int) (end int, key, root string, ok bool) {
	if index >= len(raw) || !isProjectionIdentifierStart(raw[index]) {
		return index, "", "", false
	}
	rootEnd := index + 1
	for rootEnd < len(raw) && isProjectionIdentifierContinue(raw[rootEnd]) {
		rootEnd++
	}
	root = raw[index:rootEnd]
	if rootEnd >= len(raw) || raw[rootEnd] != '.' {
		return index, "", "", false
	}
	end, key, ok = projectionPathAt(raw, rootEnd)
	return end, key, root, ok
}

func projectionHasPotentialTypedInputPath(raw string) bool {
	_, body, _, err := splitLeadingProjectionLets(raw)
	if err != nil {
		return false
	}
	entries, err := projectionNonSpreadEntries(body)
	if err != nil {
		return false
	}
	body = strings.Join(entries, ", ")
	if strings.TrimSpace(body) == "" {
		return false
	}
	found := false
	scanner := projectionShorthandScanner{
		raw: body, lastSignificant: '{', containers: []byte{'{'}, potentialTypedRootPath: &found,
	}
	for index := 0; index < len(body) && !found; {
		index = scanner.rewriteAt(index)
	}
	return found
}

func projectionSpreadRefEndsAt(raw string, end int) bool {
	return end == len(raw) || isProjectionWhitespace(raw[end]) || raw[end] == ','
}

func validateProjectionInputSpreadSpacing(raw string) error {
	for _, span := range splitProjectionEntrySpans(raw) {
		entry := raw[span.start:span.end]
		if projectionEntryIsTrivia(entry) {
			continue
		}
		if _, _, _, err := parseProjectionSpreadEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func parseProjectionSpreadEntry(entry string) (source, fields string, spread bool, err error) {
	trimmed := strings.TrimSpace(entry)
	if !strings.HasPrefix(trimmed, "...") {
		return "", "", false, nil
	}

	after := trimmed[3:]
	if after != "" && isProjectionWhitespace(after[0]) {
		path := strings.TrimLeft(after, " \t\n\r")
		if end, ok := projectionInputPathAt(path); ok && projectionSpreadRefEndsAt(path, end) {
			return "", "", true, errors.New(`projection: original-input spreads must use "...input.path" with no whitespace after "..."`)
		}
	}

	if end, ok := projectionInputPathAt(after); ok && projectionSpreadRefEndsAt(after, end) {
		source = after[:end]
		after = after[end:]
	} else {
		if after != "" && !isProjectionWhitespace(after[0]) && after[0] != '{' {
			return "", "", false, nil
		}
		after = strings.TrimLeft(after, " \t\n\r")
		if end, _, ok := projectionPathAt(after, 0); ok && projectionSpreadRefEndsAt(after, end) {
			source = after[:end]
			after = after[end:]
		} else {
			source = expressionRootName
		}
	}

	after = strings.TrimLeft(after, " \t\n\r")
	if projectionEntryIsTrivia(after) {
		return source, "", true, nil
	}
	if strings.HasPrefix(after, "{") {
		closeIndex, scanErr := scanBalancedBraces(after, 0)
		if scanErr != nil {
			return "", "", true, fmt.Errorf("projection: invalid spread fields: %w", scanErr)
		}
		if projectionTailIsTrivia(after, closeIndex+1) {
			return source, strings.TrimSpace(after[1:closeIndex]), true, nil
		}
	}
	// Preserve the legacy `... field: value` shorthand as a spread followed
	// by ordinary projection fields.
	return source, strings.TrimSpace(after), true, nil
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
