package compiler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nexssp/flow/internal/lexical"
)

func parseAtBrace(raw string, base Position) (map[string]*Value, []*Value, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]*Value{}, nil, nil
	}
	b := &braceScanner{src: raw, base: base}
	out := make(map[string]*Value)
	var spreads []*Value
	if err := b.parseEntries(out, &spreads); err != nil {
		return nil, nil, err
	}
	return out, spreads, nil
}

type braceScanner struct {
	src   string
	pos   int
	base  Position
	depth int
}

func (b *braceScanner) eof() bool { return b.pos >= len(b.src) }
func (b *braceScanner) peek() byte {
	if b.eof() {
		return 0
	}
	return b.src[b.pos]
}
func (b *braceScanner) advance() { b.pos++ }

func (b *braceScanner) errf(format string, args ...any) error {
	return SourceError(b.base, "%s", "(...): "+fmt.Sprintf(format, args...))
}

func (b *braceScanner) skipWS() {
	for !b.eof() {
		c := b.peek()
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			b.advance()
			continue
		}
		if b.skipComment() {
			continue
		}
		return
	}
}

func (b *braceScanner) skipComment() bool {
	if !b.commentStarts() {
		return false
	}
	if b.peek() == '/' {
		for !b.eof() && b.peek() != '\n' {
			b.advance()
		}
		return true
	}
	if b.peek() != '#' || !b.hashStartsComment() {
		return false
	}
	for !b.eof() && b.peek() != '\n' {
		b.advance()
	}
	return true
}

func (b *braceScanner) commentStarts() bool {
	return !b.eof() && ((b.peek() == '/' && b.pos+1 < len(b.src) && b.src[b.pos+1] == '/') ||
		(b.peek() == '#' && b.hashStartsComment()))
}

func (b *braceScanner) hashStartsComment() bool {
	if b.pos+1 < len(b.src) {
		next := b.src[b.pos+1]
		if next == ' ' || next == '\t' || next == '\r' || next == '\n' {
			return true
		}
	}
	for i := b.pos - 1; i >= 0 && b.src[i] != '\n'; i-- {
		if b.src[i] != ' ' && b.src[i] != '\t' && b.src[i] != '\r' {
			return false
		}
	}
	return true
}

func (b *braceScanner) parseEntries(dst map[string]*Value, spreads *[]*Value) error {
	for {
		b.skipWS()
		if b.eof() {
			return nil
		}
		if strings.HasPrefix(b.src[b.pos:], "...") {
			b.pos += 3
			value, err := b.readValue()
			if err != nil {
				return err
			}
			*spreads = append(*spreads, value)
			if err := b.consumeEntryDelimiter(); err != nil {
				return err
			}
			continue
		}
		key, err := b.readKey()
		if err != nil {
			return err
		}
		derivedKey, pathRef, pathShorthand, err := argumentPathShorthand(key)
		if err != nil {
			return b.errf("%v", err)
		}
		b.skipWS()
		var val *Value
		switch {
		case b.peek() == ':':
			if pathShorthand {
				return b.errf("path shorthand %q must not have an explicit ':' value", key)
			}
			b.advance()
			val, err = b.readValue()
			if err != nil {
				return err
			}
		case b.eof() || b.peek() == ',' || b.peek() == '}':
			if pathShorthand {
				key = derivedKey
				val = &Value{Kind: ValueRef, Ref: pathRef}
			} else {
				val = &Value{Kind: ValueRef, Ref: key}
			}
		default:
			return b.errf("expected ':' or map-entry delimiter after key %q", key)
		}
		if _, exists := dst[key]; exists {
			return b.errf("duplicate key %q", key)
		}
		dst[key] = val
		if err := b.consumeEntryDelimiter(); err != nil {
			return err
		}
	}
}

// argumentPathShorthand recognizes a standalone input path such as
// `.current.attempt.attempt_id` and returns its final segment as the key.
func argumentPathShorthand(path string) (key, ref string, shorthand bool, err error) {
	if !strings.HasPrefix(path, ".") {
		return "", "", false, nil
	}
	ref = strings.TrimPrefix(path, ".")
	segments := strings.Split(ref, ".")
	for _, segment := range segments {
		if segment == "" || !isFunctionIdentifierStart(segment[0]) {
			return "", "", true, fmt.Errorf("invalid path shorthand %q", path)
		}
		for i := 1; i < len(segment); i++ {
			if !isFunctionIdentifierContinue(segment[i]) && segment[i] != '-' {
				return "", "", true, fmt.Errorf("invalid path shorthand %q", path)
			}
		}
	}
	return segments[len(segments)-1], ref, true, nil
}

func (b *braceScanner) consumeEntryDelimiter() error {
	b.skipWS()
	if b.eof() || b.peek() == '}' {
		return nil
	}
	if b.peek() == ',' {
		b.advance()
		return nil
	}
	return b.errf("expected ',' or '}', got %q", string(b.peek()))
}

func (b *braceScanner) readKey() (string, error) {
	start := b.pos
	for !b.eof() {
		c := b.peek()
		if c == ':' || c == ',' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			break
		}
		b.advance()
	}
	key := strings.TrimSpace(b.src[start:b.pos])
	if key == "" {
		return "", b.errf("empty key")
	}
	return key, nil
}

func (b *braceScanner) readValue() (*Value, error) {
	b.skipWS()
	if b.eof() {
		return nil, b.errf("expected value")
	}
	start := b.pos
	switch b.peek() {
	case '{':
		value, err := b.readObject()
		if err != nil || b.valueDelimiterAfterCurrent() {
			return value, err
		}
	case '[':
		value, err := b.readArray()
		if err != nil || b.valueDelimiterAfterCurrent() {
			return value, err
		}
	case '(':
		value, err := b.readParenthesizedExpression()
		if err != nil || b.valueDelimiterAfterCurrent() {
			return value, err
		}
	}
	b.pos = start
	return b.readImplicitValue()
}

func (b *braceScanner) valueDelimiterAfterCurrent() bool {
	position := b.pos
	for position < len(b.src) {
		if isFunctionWhitespace(b.src[position]) {
			position++
			continue
		}
		if position+1 < len(b.src) && b.src[position] == '/' {
			switch b.src[position+1] {
			case '/':
				for position < len(b.src) && b.src[position] != '\n' {
					position++
				}
				continue
			case '*':
				commentEnd := strings.Index(b.src[position+2:], "*/")
				if commentEnd < 0 {
					return false
				}
				position += commentEnd + 4
				continue
			}
		}
		if b.src[position] == '#' && b.hashStartsCommentAt(position) {
			for position < len(b.src) && b.src[position] != '\n' {
				position++
			}
			continue
		}
		break
	}
	return position == len(b.src) || b.src[position] == ',' || b.src[position] == '}' || b.src[position] == ']' || b.src[position] == ')'
}

func (b *braceScanner) readImplicitValue() (*Value, error) {
	start := b.pos
	end, err := b.implicitValueEnd(start)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(b.src[start:end])
	b.pos = end
	if raw == "" {
		return nil, b.errf("expected value")
	}
	if isFunctionExpressionQuote(raw[0]) {
		b.pos = start
		value, quoteErr := b.readQuoted()
		if quoteErr != nil {
			return nil, quoteErr
		}
		if b.pos == start+len(raw) {
			return value, nil
		}
		b.pos = end
	}
	if value, ok, invalidNumber := parseImplicitLiteral(raw); ok {
		return value, nil
	} else if invalidNumber {
		return nil, b.errf("invalid numeric literal %q", raw)
	}
	if (raw[0] == '.' || raw[0] == '@') && isBareReference(raw[1:]) {
		return &Value{Kind: ValueRef, Ref: raw[1:]}, nil
	}
	if isBareIdentifierValue(raw) {
		return &Value{Kind: ValueBare, Str: raw}, nil
	}
	return &Value{Kind: ValueExpression, Expression: raw}, nil
}

func parseImplicitLiteral(raw string) (*Value, bool, bool) {
	switch raw {
	case "true":
		return &Value{Kind: ValueBool, Bool: true}, true, false
	case "false":
		return &Value{Kind: ValueBool, Bool: false}, true, false
	case "null", "nil":
		return &Value{Kind: ValueNull}, true, false
	}
	if raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9' {
		normalized, err := normalizeNumericSeparators(raw)
		if err != nil && isNumericLiteralCandidate(raw) {
			return nil, false, true
		}
		if err == nil {
			if number, parseErr := strconv.ParseFloat(normalized, 64); parseErr == nil {
				return &Value{Kind: ValueNumber, Num: number}, true, false
			}
		}
	}
	return nil, false, false
}

func (b *braceScanner) implicitValueEnd(start int) (int, error) {
	depth := functionExpressionDepth{}
	for position := start; position < len(b.src); position++ {
		current := b.src[position]
		if depth.quote != 0 {
			position = skipFunctionExpressionQuote(b.src, position, &depth.quote)
			continue
		}
		if isFunctionExpressionQuote(current) {
			depth.quote = current
			continue
		}
		next, handled, endExpression, err := b.skipImplicitValueComment(position, current, &depth)
		if err != nil {
			return position, err
		}
		if endExpression {
			return position, nil
		}
		if handled {
			position = next
			continue
		}
		if depth.topLevel() && isImplicitValueDelimiter(current) {
			return position, nil
		}
		consumeExpressionDelimiter(&depth, current)
	}
	return len(b.src), nil
}

func (b *braceScanner) skipImplicitValueComment(position int, current byte, depth *functionExpressionDepth) (next int, handled, endExpression bool, err error) {
	if current == '/' && position+1 < len(b.src) {
		switch b.src[position+1] {
		case '/':
			if depth.topLevel() {
				return position, true, true, nil
			}
			next = position + 2
			for next < len(b.src) && b.src[next] != '\n' {
				next++
			}
			return next - 1, true, false, nil
		case '*':
			commentEnd := strings.Index(b.src[position+2:], "*/")
			if commentEnd < 0 {
				return position, true, false, b.errf("unclosed block comment in expression")
			}
			return position + commentEnd + 3, true, false, nil
		}
	}
	if current == '#' && b.hashStartsCommentAt(position) {
		if depth.topLevel() {
			return position, true, true, nil
		}
		next = position + 1
		for next < len(b.src) && b.src[next] != '\n' {
			next++
		}
		return next - 1, true, false, nil
	}
	return position, false, false, nil
}

func isImplicitValueDelimiter(current byte) bool {
	switch current {
	case ',', '}', ']', ')':
		return true
	default:
		return false
	}
}

func consumeExpressionDelimiter(depth *functionExpressionDepth, current byte) {
	switch current {
	case '(':
		depth.parentheses++
	case ')':
		if depth.parentheses > 0 {
			depth.parentheses--
		}
	case '[':
		depth.brackets++
	case ']':
		if depth.brackets > 0 {
			depth.brackets--
		}
	case '{':
		depth.braces++
	case '}':
		if depth.braces > 0 {
			depth.braces--
		}
	}
}

func isBareReference(raw string) bool {
	if raw == "" {
		return true
	}
	for index := range raw {
		current := raw[index]
		if current != '.' && current != '-' && !isFunctionIdentifierContinue(current) {
			return false
		}
	}
	return true
}

func isBareIdentifierValue(raw string) bool {
	if raw == "" || !isFunctionIdentifierStart(raw[0]) {
		return false
	}
	for index := 1; index < len(raw); index++ {
		current := raw[index]
		if current == '.' || current == '-' || isFunctionIdentifierContinue(current) {
			continue
		}
		return false
	}
	return true
}

func isNumericLiteralCandidate(raw string) bool {
	if raw == "" || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return false
	}
	for index, current := range raw {
		if index == 0 && (current == '-' || current == '+') {
			continue
		}
		if current >= '0' && current <= '9' || current >= 'a' && current <= 'f' || current >= 'A' && current <= 'F' || strings.ContainsRune("_ .+eEpPxXbBoO", current) {
			continue
		}
		return false
	}
	return true
}

type functionExpressionDepth struct {
	quote       byte
	parentheses int
	brackets    int
	braces      int
}

func (depth *functionExpressionDepth) topLevel() bool {
	return depth.parentheses == 0 && depth.brackets == 0 && depth.braces == 0
}

func skipFunctionExpressionQuote(source string, index int, quote *byte) int {
	current := source[index]
	if current == '\\' && *quote != '`' && index+1 < len(source) {
		return index + 1
	}
	if current == *quote {
		*quote = 0
	}
	return index
}

func isFunctionExpressionQuote(value byte) bool {
	return value == '"' || value == '\'' || value == '`'
}

func (b *braceScanner) readParenthesizedExpression() (*Value, error) {
	start := b.pos + 1
	depth := functionExpressionDepth{parentheses: 1}
	var expressionBuilder strings.Builder
	expressionStart := start
	hasHashComment := false
	if b.depth+depth.parentheses > maxParseDepth {
		return nil, b.errf("parse depth exceeded (%d)", maxParseDepth)
	}
	for scan := start; scan < len(b.src); scan++ {
		current := b.src[scan]
		if depth.quote != 0 {
			scan = skipFunctionExpressionQuote(b.src, scan, &depth.quote)
			continue
		}
		if isFunctionExpressionQuote(current) {
			depth.quote = current
			continue
		}
		commentEnd, isComment, err := b.parenthesizedExpressionCommentEnd(scan)
		if err != nil {
			return nil, err
		}
		if isComment {
			scan = commentEnd
			continue
		}
		if commentEnd, isComment := b.parenthesizedHashCommentEnd(scan, depth); isComment {
			if !hasHashComment {
				expressionBuilder.WriteString(b.src[start:scan])
				hasHashComment = true
			} else {
				expressionBuilder.WriteString(b.src[expressionStart:scan])
			}
			if commentEnd < len(b.src) {
				expressionBuilder.WriteByte('\n')
			}
			expressionStart = commentEnd + 1
			scan = commentEnd
			continue
		}
		closed, err := b.consumeParenthesizedDelimiter(current, &depth)
		if err != nil {
			return nil, err
		}
		if closed {
			expressionSource := b.src[start:scan]
			if hasHashComment {
				expressionBuilder.WriteString(b.src[expressionStart:scan])
				expressionSource = expressionBuilder.String()
			}
			expression := strings.TrimSpace(expressionSource)
			if expression == "" {
				return nil, b.errf("empty parenthesized expression")
			}
			b.pos = scan + 1
			return &Value{Kind: ValueExpression, Expression: expression}, nil
		}
		if b.depth+depth.parentheses+depth.brackets+depth.braces > maxParseDepth {
			return nil, b.errf("parse depth exceeded (%d)", maxParseDepth)
		}
	}
	return nil, b.errf("unclosed parenthesized expression")
}

func (b *braceScanner) parenthesizedExpressionCommentEnd(position int) (end int, isComment bool, err error) {
	end = position
	if position+1 >= len(b.src) || b.src[position] != '/' {
		return end, false, nil
	}
	switch b.src[position+1] {
	case '/':
		for end < len(b.src) && b.src[end] != '\n' {
			end++
		}
		return end, true, nil
	case '*':
		commentEnd := strings.Index(b.src[position+2:], "*/")
		if commentEnd < 0 {
			return position, true, b.errf("unclosed block comment in parenthesized expression")
		}
		return position + commentEnd + 3, true, nil
	default:
		return end, false, nil
	}
}

func (b *braceScanner) parenthesizedHashCommentEnd(position int, depth functionExpressionDepth) (end int, isComment bool) {
	end = position
	if b.src[position] != '#' || depth.parentheses != 1 || depth.brackets != 0 || depth.braces != 0 || !b.hashStartsCommentAt(position) {
		return end, false
	}
	for end < len(b.src) {
		if b.src[end] == '\n' {
			return end, true
		}
		end++
	}
	return end, true
}

func (b *braceScanner) consumeParenthesizedDelimiter(current byte, depth *functionExpressionDepth) (closed bool, err error) {
	switch current {
	case '(':
		depth.parentheses++
	case ')':
		if depth.parentheses == 0 {
			return false, b.errf("unmatched ')' in parenthesized expression")
		}
		if depth.parentheses == 1 {
			if depth.brackets != 0 || depth.braces != 0 {
				return false, b.errf("unclosed delimiter in parenthesized expression")
			}
			return true, nil
		}
		depth.parentheses--
	case '[':
		depth.brackets++
	case ']':
		if depth.brackets == 0 {
			return false, b.errf("unmatched ']' in parenthesized expression")
		}
		depth.brackets--
	case '{':
		depth.braces++
	case '}':
		if depth.braces == 0 {
			return false, b.errf("unclosed parenthesized expression")
		}
		depth.braces--
	}
	return false, nil
}

func (b *braceScanner) hashStartsCommentAt(position int) bool {
	if position+1 < len(b.src) {
		next := b.src[position+1]
		if next != ' ' && next != '\t' && next != '\r' && next != '\n' {
			return false
		}
	}
	if position == b.pos {
		return true
	}
	for index := position - 1; index >= 0 && b.src[index] != '\n'; index-- {
		previous := b.src[index]
		if previous == ' ' || previous == '\t' || previous == '\r' {
			continue
		}
		switch previous {
		case ',', '(', '[', '{', ':', '?', '+', '-', '*', '/', '%', '&', '|', '=', '!', '<', '>':
			return false
		default:
			return true
		}
	}
	return true
}

func isFunctionIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isFunctionIdentifierContinue(value byte) bool {
	return isFunctionIdentifierStart(value) || value >= '0' && value <= '9'
}

func isFunctionWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func (b *braceScanner) readObject() (*Value, error) {
	if b.depth >= maxParseDepth {
		return nil, b.errf("parse depth exceeded (%d)", maxParseDepth)
	}
	b.depth++
	defer func() { b.depth-- }()
	b.advance()
	v := &Value{Kind: ValueMap}
	keys := make(map[string]struct{})
	for {
		b.skipWS()
		if b.peek() == '}' {
			b.advance()
			return v, nil
		}
		key, err := b.readKey()
		if err != nil {
			return nil, err
		}
		derivedKey, pathRef, pathShorthand, err := argumentPathShorthand(key)
		if err != nil {
			return nil, b.errf("%v", err)
		}
		b.skipWS()
		var val *Value
		switch b.peek() {
		case ':':
			if pathShorthand {
				return nil, b.errf("path shorthand %q must not have an explicit ':' value", key)
			}
			b.advance()
			val, err = b.readValue()
			if err != nil {
				return nil, err
			}
		case ',', '}':
			if pathShorthand {
				key = derivedKey
				val = &Value{Kind: ValueRef, Ref: pathRef}
			} else {
				val = &Value{Kind: ValueRef, Ref: key}
			}
		default:
			return nil, b.errf("expected ':' or map-entry delimiter in nested object")
		}
		if _, exists := keys[key]; exists {
			return nil, b.errf("duplicate key %q in nested object", key)
		}
		keys[key] = struct{}{}
		v.Map = append(v.Map, MapEntry{Key: key, Value: val})
		b.skipWS()
		switch b.peek() {
		case ',':
			b.advance()
		case '}':
			b.advance()
			return v, nil
		default:
			return nil, b.errf("expected ',' or '}' in object")
		}
	}
}

func (b *braceScanner) readArray() (*Value, error) {
	if b.depth >= maxParseDepth {
		return nil, b.errf("parse depth exceeded (%d)", maxParseDepth)
	}
	b.depth++
	defer func() { b.depth-- }()
	b.advance()
	v := &Value{Kind: ValueSlice}
	for {
		b.skipWS()
		if b.peek() == ']' {
			b.advance()
			return v, nil
		}
		elem, err := b.readValue()
		if err != nil {
			return nil, err
		}
		v.Slice = append(v.Slice, elem)
		b.skipWS()
		switch b.peek() {
		case ',':
			b.advance()
		case ']':
			b.advance()
			return v, nil
		default:
			return nil, b.errf("expected ',' or ']' in array")
		}
	}
}

func (b *braceScanner) readQuoted() (*Value, error) {
	q := b.peek()
	start := b.pos
	b.advance()
	for !b.eof() {
		c := b.peek()
		if lexical.BackslashEscapesNext(q, c, b.pos+1 < len(b.src)) {
			b.pos += 2
			continue
		}
		if c == q {
			b.advance()
			if q == '"' {
				value, err := lexical.DecodeDoubleQuotedString(b.src[start:b.pos])
				if err != nil {
					return nil, b.errf("invalid string literal: %v", err)
				}
				return &Value{Kind: ValueString, Str: value}, nil
			}
			return &Value{Kind: ValueString, Str: b.src[start+1 : b.pos-1]}, nil
		}
		b.advance()
	}
	return nil, b.errf("unclosed quote")
}
