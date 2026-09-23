package compiler

import (
	"strconv"
	"strings"
)

// parseAtBrace parses the content of an @{ ... } block into typed
// values and installs them in atom.Args.
//
// The grammar is a small JSON-like language extended with references:
//
//	block   := (entry (',' entry)*)? ','?
//	entry   := key ':' value
//	key     := ident
//	value   := object | array | string | number | bool | ref | bare
//	object  := '{' (entry (',' entry)*)? '}'
//	array   := '[' (value (',' value)*)? ']'
//	ref     := '.' ident ('.' ident)*    |   '.'
//	string  := '"' ... '"' | "'" ... "'" | '`' ... '`'
//	number  := '-'? digit+ ('.' digit+)?
//	bool    := true | false | null
//	bare    := any run of characters not in {',', '}', ']'}
//
// Bare words (like `billing` in `type: billing`) are treated as string
// literals. This preserves the existing behaviour where unquoted and
// quoted literals are equivalent, while adding full structural support.
//
// Line comments (`//`, `#`) and block comments (`/* */`) are allowed
// between tokens, so users can annotate nested blocks.
func parseAtBrace(atom *AtomExpr, raw string, pos Position) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	bs := &braceScanner{
		src:  raw,
		line: pos.Line,
		col:  pos.Col,
		file: pos.File,
	}
	return bs.parseEntries(atom.Args)
}

// braceScanner is a recursive-descent parser over the raw contents of
// an @{ ... } block. It has no knowledge of the outer DSL grammar.
type braceScanner struct {
	src  string
	pos  int
	line int
	col  int
	file string
}

func (b *braceScanner) eof() bool { return b.pos >= len(b.src) }

func (b *braceScanner) peek() byte {
	if b.eof() {
		return 0
	}
	return b.src[b.pos]
}

func (b *braceScanner) advance() {
	if b.eof() {
		return
	}
	if b.src[b.pos] == '\n' {
		b.line++
		b.col = 1
	} else {
		b.col++
	}
	b.pos++
}

func (b *braceScanner) skipWS() {
	for !b.eof() {
		c := b.peek()
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			b.advance()
			continue
		}
		if c == '#' {
			for !b.eof() && b.peek() != '\n' {
				b.advance()
			}
			continue
		}
		if c == '/' && b.pos+1 < len(b.src) {
			next := b.src[b.pos+1]
			if next == '/' {
				for !b.eof() && b.peek() != '\n' {
					b.advance()
				}
				continue
			}
			if next == '*' {
				b.advance()
				b.advance()
				for !b.eof() {
					if b.peek() == '*' && b.pos+1 < len(b.src) && b.src[b.pos+1] == '/' {
						b.advance()
						b.advance()
						break
					}
					b.advance()
				}
				continue
			}
		}
		return
	}
}

func (b *braceScanner) errf(format string, args ...any) error {
	return SourceError(Position{
		File: b.file,
		Line: b.line,
		Col:  b.col,
	}, format, args...)
}

// parseEntries reads a comma-separated list of `key: value` pairs into
// dst. Stops at the first unmatched '}' or end of input.
func (b *braceScanner) parseEntries(dst map[string]*Value) error {
	for {
		b.skipWS()
		if b.eof() || b.peek() == '}' {
			return nil
		}

		key, err := b.readKey()
		if err != nil {
			return err
		}

		b.skipWS()
		if b.eof() || b.peek() != ':' {
			return b.errf("expected ':' after key %q in @{ ... }", key)
		}
		b.advance()

		val, err := b.readValue()
		if err != nil {
			return err
		}

		if _, dup := dst[key]; dup {
			return b.errf("duplicate key %q in @{ ... }", key)
		}
		dst[key] = val

		b.skipWS()
		if b.eof() {
			return nil
		}
		switch b.peek() {
		case ',':
			b.advance()
		case '}':
			return nil
		default:
			return b.errf("expected ',' or '}' after value for %q, got %q",
				key, string(b.peek()))
		}
	}
}

func (b *braceScanner) readKey() (string, error) {
	start := b.pos
	for !b.eof() {
		c := b.peek()
		if c == ':' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			break
		}
		b.advance()
	}
	key := strings.TrimSpace(b.src[start:b.pos])
	if key == "" {
		return "", b.errf("empty key in @{ ... }")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		ok := c == '_' || c == '-' || c == '.' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9')
		if !ok {
			return "", b.errf("invalid character %q in key %q", string(c), key)
		}
	}
	return key, nil
}

func (b *braceScanner) readValue() (*Value, error) {
	b.skipWS()
	if b.eof() {
		return nil, b.errf("expected value in @{ ... }")
	}

	c := b.peek()
	switch {
	case c == '{':
		return b.readObject()
	case c == '[':
		return b.readArray()
	case c == '"' || c == '\'' || c == '`':
		return b.readQuoted()
	case c == '.':
		return b.readRef()
	case c == '-' || (c >= '0' && c <= '9'):
		return b.readNumberOrBare()
	case c == 't' || c == 'f' || c == 'n':
		return b.readKeywordOrBare()
	default:
		return b.readBare()
	}
}

func (b *braceScanner) readObject() (*Value, error) {
	b.advance() // '{'
	v := &Value{Kind: ValueMap}
	seen := map[string]bool{}

	for {
		b.skipWS()
		if b.eof() {
			return nil, b.errf("unclosed '{' in @{ ... }")
		}
		if b.peek() == '}' {
			b.advance()
			return v, nil
		}

		key, err := b.readKey()
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, b.errf("duplicate key %q in nested object", key)
		}
		seen[key] = true

		b.skipWS()
		if b.eof() || b.peek() != ':' {
			return nil, b.errf("expected ':' after key %q in nested object", key)
		}
		b.advance()

		val, err := b.readValue()
		if err != nil {
			return nil, err
		}
		v.Map = append(v.Map, MapEntry{Key: key, Value: val})

		b.skipWS()
		if b.eof() {
			return nil, b.errf("unclosed '{' in @{ ... }")
		}
		switch b.peek() {
		case ',':
			b.advance()
		case '}':
			b.advance()
			return v, nil
		default:
			return nil, b.errf("expected ',' or '}' in nested object, got %q",
				string(b.peek()))
		}
	}
}

func (b *braceScanner) readArray() (*Value, error) {
	b.advance() // '['
	v := &Value{Kind: ValueSlice}
	for {
		b.skipWS()
		if b.eof() {
			return nil, b.errf("unclosed '[' in @{ ... }")
		}
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
		if b.eof() {
			return nil, b.errf("unclosed '[' in @{ ... }")
		}
		switch b.peek() {
		case ',':
			b.advance()
		case ']':
			b.advance()
			return v, nil
		default:
			return nil, b.errf("expected ',' or ']' in array, got %q",
				string(b.peek()))
		}
	}
}

func (b *braceScanner) readQuoted() (*Value, error) {
	quote := b.peek()
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
		if c == quote {
			b.advance()
			return &Value{Kind: ValueString, Str: sb.String()}, nil
		}
		sb.WriteByte(c)
		b.advance()
	}
	return nil, b.errf("unclosed quote in @{ ... }")
}

func (b *braceScanner) readRef() (*Value, error) {
	b.advance() // '.'
	start := b.pos
	for !b.eof() {
		c := b.peek()
		ok := c == '_' || c == '.' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9')
		if !ok {
			break
		}
		b.advance()
	}
	ref := b.src[start:b.pos]
	return &Value{Kind: ValueRef, Ref: ref}, nil
}

func (b *braceScanner) readNumberOrBare() (*Value, error) {
	raw := b.readBareToken()
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return &Value{Kind: ValueNumber, Num: f}, nil
	}
	return &Value{Kind: ValueString, Str: raw}, nil
}

func (b *braceScanner) readKeywordOrBare() (*Value, error) {
	raw := b.readBareToken()
	switch raw {
	case "true":
		return &Value{Kind: ValueBool, Bool: true}, nil
	case "false":
		return &Value{Kind: ValueBool, Bool: false}, nil
	case "null":
		return &Value{Kind: ValueNull}, nil
	}
	return &Value{Kind: ValueString, Str: raw}, nil
}

func (b *braceScanner) readBare() (*Value, error) {
	raw := b.readBareToken()
	if raw == "" {
		return nil, b.errf("empty value in @{ ... }")
	}
	return &Value{Kind: ValueString, Str: raw}, nil
}

// readBareToken consumes characters until a structural delimiter.
func (b *braceScanner) readBareToken() string {
	start := b.pos
	for !b.eof() {
		c := b.peek()
		if c == ',' || c == '}' || c == ']' {
			break
		}
		b.advance()
	}
	return strings.TrimSpace(b.src[start:b.pos])
}
