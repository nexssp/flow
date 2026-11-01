package compiler

import (
	"fmt"
	"strconv"
	"strings"
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
	return SourceError(b.base, "%s", "@{...}: "+fmt.Sprintf(format, args...))
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
		b.skipWS()
		var val *Value
		switch {
		case b.peek() == ':':
			b.advance()
			val, err = b.readValue()
			if err != nil {
				return err
			}
		case b.eof() || b.peek() == ',' || b.peek() == '}':
			val = &Value{Kind: ValueRef, Ref: key}
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
	c := b.peek()
	switch {
	case c == '{':
		return b.readObject()
	case c == '[':
		return b.readArray()
	case c == '"' || c == '\'' || c == '`':
		return b.readQuoted()
	case c == '.' || c == '@':
		return b.readRef()
	case c == '-' || (c >= '0' && c <= '9'):
		return b.readNumber()
	case c == 't' || c == 'f' || c == 'n':
		return b.readKeyword()
	default:
		if isFunctionIdentifierStart(c) {
			if value, matched, err := b.readFunctionExpression(); matched || err != nil {
				return value, err
			}
		}
		return b.readBare()
	}
}

func (b *braceScanner) readFunctionExpression() (*Value, bool, error) {
	start := b.pos
	index := start
	hasNamespace := false
	for index < len(b.src) {
		if !isFunctionIdentifierStart(b.src[index]) {
			return nil, false, nil
		}
		index++
		for index < len(b.src) && isFunctionIdentifierContinue(b.src[index]) {
			index++
		}
		if index >= len(b.src) || b.src[index] != '.' || index+1 >= len(b.src) || !isFunctionIdentifierStart(b.src[index+1]) {
			break
		}
		hasNamespace = true
		index++
	}
	if !hasNamespace {
		return nil, false, nil
	}
	for index < len(b.src) && isFunctionWhitespace(b.src[index]) {
		index++
	}
	if index >= len(b.src) || b.src[index] != '(' {
		return nil, false, nil
	}
	return b.functionExpressionValue(start, b.functionExpressionEnd(start))
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

func (depth *functionExpressionDepth) consumeDelimiter(current byte) bool {
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
		if depth.brackets == 0 {
			return true
		}
		depth.brackets--
	case '{':
		depth.braces++
	case '}':
		if depth.braces == 0 {
			return true
		}
		depth.braces--
	case ',':
		return depth.topLevel()
	}
	return false
}

func (b *braceScanner) functionExpressionEnd(start int) int {
	var depth functionExpressionDepth
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
		if b.functionExpressionCommentAt(scan) && depth.topLevel() {
			break
		}
		if depth.consumeDelimiter(current) {
			return scan
		}
	}
	return len(b.src)
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

func (b *braceScanner) functionExpressionCommentAt(index int) bool {
	current := b.src[index]
	return current == '#' && b.hashStartsCommentAt(index) ||
		current == '/' && index+1 < len(b.src) && b.src[index+1] == '/'
}

func (b *braceScanner) functionExpressionValue(start, end int) (*Value, bool, error) {
	expression := strings.TrimSpace(b.src[start:end])
	b.pos = end
	if expression == "" {
		return nil, true, b.errf("empty function expression")
	}
	return &Value{Kind: ValueExpression, Expression: expression}, true, nil
}

func (b *braceScanner) hashStartsCommentAt(position int) bool {
	if position != b.pos {
		for i := position - 1; i >= 0 && b.src[i] != '\n'; i-- {
			if b.src[i] != ' ' && b.src[i] != '\t' && b.src[i] != '\r' {
				return false
			}
		}
	}
	if position+1 < len(b.src) {
		next := b.src[position+1]
		return next == ' ' || next == '\t' || next == '\r' || next == '\n'
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
		b.skipWS()
		var val *Value
		switch b.peek() {
		case ':':
			b.advance()
			val, err = b.readValue()
			if err != nil {
				return nil, err
			}
		case ',', '}':
			val = &Value{Kind: ValueRef, Ref: key}
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
	b.advance()
	var sb strings.Builder
	for !b.eof() {
		c := b.peek()
		if c == '\\' && b.pos+1 < len(b.src) {
			sb.WriteByte(c)
			b.advance()
			sb.WriteByte(b.peek())
			b.advance()
			continue
		}
		if c == q {
			b.advance()
			return &Value{Kind: ValueString, Str: sb.String()}, nil
		}
		sb.WriteByte(c)
		b.advance()
	}
	return nil, b.errf("unclosed quote")
}

func (b *braceScanner) readRef() (*Value, error) {
	b.advance() // '.' or '@'
	start := b.pos
	for !b.eof() {
		c := b.peek()
		ok := c == '_' || c == '.' || c == '-' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			break
		}
		b.advance()
	}
	ref := b.src[start:b.pos]
	return &Value{Kind: ValueRef, Ref: ref}, nil
}

func (b *braceScanner) readNumber() (*Value, error) {
	raw := b.readBareToken()
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return &Value{Kind: ValueNumber, Num: f}, nil
	}
	return &Value{Kind: ValueBare, Str: raw}, nil
}

func (b *braceScanner) readKeyword() (*Value, error) {
	raw := b.readBareToken()
	switch raw {
	case "true":
		return &Value{Kind: ValueBool, Bool: true}, nil
	case "false":
		return &Value{Kind: ValueBool, Bool: false}, nil
	case "null":
		return &Value{Kind: ValueNull}, nil
	}
	return &Value{Kind: ValueBare, Str: raw}, nil
}

func (b *braceScanner) readBare() (*Value, error) {
	raw := b.readBareToken()
	if raw == "" {
		return nil, b.errf("empty value")
	}
	return &Value{Kind: ValueBare, Str: raw}, nil
}

func (b *braceScanner) readBareToken() string {
	start := b.pos
	for !b.eof() {
		c := b.peek()
		if c == ',' || c == '}' || c == ']' {
			break
		}
		if b.commentStarts() {
			break
		}
		b.advance()
	}
	return strings.TrimSpace(b.src[start:b.pos])
}
