package core

import (
	"context"
	"fmt"
	"maps"
	"strings"
)

type Parser struct {
	ctx       context.Context
	file      string
	lineBase  int
	src       string
	toks      []Token
	pos       int
	ops       *OperatorTable
	primaries *PrimaryExtensionTable
	depth     int
}

const maxParseDepth = 64

// NewParser is the no-file convenience used in tests. Parse errors
// report only the line number.
func NewParser(ctx context.Context, ops *OperatorTable, src string) *Parser {
	return NewParserWithFileOffset(ctx, ops, DefaultPrimaryExtensions(), src, "", 0)
}

// NewParserWithPrimaries is kept for compatibility with callers that
// pass a primaries table explicitly.
func NewParserWithPrimaries(
	ctx context.Context,
	ops *OperatorTable,
	primaries *PrimaryExtensionTable,
	src string,
) *Parser {
	return NewParserWithFileOffset(ctx, ops, primaries, src, "", 0)
}

// NewParserWithFile carries the source path so parse errors are
// formatted as `file:line`, which terminals make clickable.
func NewParserWithFile(
	ctx context.Context,
	ops *OperatorTable,
	primaries *PrimaryExtensionTable,
	src, file string,
) *Parser {
	return NewParserWithFileOffset(ctx, ops, primaries, src, file, 0)
}

// NewParserWithFileOffset additionally shifts reported line numbers by
// lineBase. Used when a fragment (e.g. a @pipeline body) is parsed in
// isolation but its errors must point at the parent file's lines.
func NewParserWithFileOffset(
	ctx context.Context,
	ops *OperatorTable,
	primaries *PrimaryExtensionTable,
	src, file string,
	lineBase int,
) *Parser {
	if primaries == nil {
		primaries = DefaultPrimaryExtensions()
	}
	var toks []Token
	l := NewLexer(src)
	for {
		t := l.Next()
		toks = append(toks, t)
		if t.Type == TokEOF {
			break
		}
	}
	return &Parser{
		ctx:       ctx,
		src:       src,
		file:      file,
		lineBase:  lineBase,
		toks:      toks,
		ops:       ops,
		primaries: primaries,
	}
}

// Cur returns the token under the cursor.
func (p *Parser) Cur() Token { return p.toks[p.pos] }

// Current is an alias for Cur, exported for external primary extensions.
func (p *Parser) Current() Token { return p.toks[p.pos] }

// Next advances the cursor by one token.
func (p *Parser) Next() {
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
}

// Advance is an alias for Next, exported for external primary extensions.
func (p *Parser) Advance() { p.Next() }

// errorf wraps a parse failure with the current position.
func (p *Parser) errorf(line int, format string, args ...any) error {
	return SourceError(Position{File: p.file, Line: line + p.lineBase}, format, args...)
}

// Fail reports a parse error at the given line offset by lineBase.
// Exported so external primary extensions can format errors the same
// way the built-in parser does.
func (p *Parser) Fail(line int, format string, args ...any) error {
	return p.errorf(line, format, args...)
}

// Src returns the source text under parse.
func (p *Parser) Src() string { return p.src }

// File returns the source file path, or "".
func (p *Parser) File() string { return p.file }

// isTernaryColon zwraca true gdy token ':' jest separatorem ternary
// (odstęp po obu stronach), a nie prefiksem modyfikatora atomu.
func (p *Parser) isTernaryColon() bool {
	if p.Cur().Type != TokColon || p.pos+1 >= len(p.toks) {
		return false
	}
	gapStart := p.Cur().Offset + 1
	gapEnd := p.toks[p.pos+1].Offset
	if gapEnd <= gapStart || gapEnd > len(p.src) {
		return false
	}
	for i := gapStart; i < gapEnd; i++ {
		switch p.src[i] {
		case ' ', '\t', '\r', '\n':
		default:
			return false
		}
	}
	return true
}

func (p *Parser) Parse() (Expr, error) {
	if p.ops == nil {
		return nil, p.errorf(p.Cur().Line, "nil operator table")
	}
	e, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.Cur().Type != TokEOF {
		return nil, p.errorf(p.Cur().Line, "unexpected token %q after expression", p.Cur().Lit)
	}
	return e, nil
}

// ParseExpr parses a sub-expression with the given minimum precedence.
// Exported so primary extensions can recurse without reimplementing
// the precedence-climbing loop.
func (p *Parser) ParseExpr(minPrec int) (Expr, error) {
	return p.parseExpr(minPrec)
}

// SubParse parses a fresh source fragment in this parser's context.
// The same operator table, primary table, and source-file metadata are
// reused. External primary extensions call it to re-enter the parser
// with substituted text.
func (p *Parser) SubParse(src string) (Expr, error) {
	if p.depth >= maxParseDepth {
		return nil, fmt.Errorf("core: parse depth exceeded (%d)", maxParseDepth)
	}
	sub := NewParserWithFileOffset(p.ctx, p.ops, p.primaries, src, p.file, p.lineBase)
	sub.depth = p.depth + 1
	return sub.Parse()
}

// parseExpr to parser precedencji z obsługą ternary na najniższym
// poziomie. Ternary jest prawostronnie łączny i nie przechodzi przez
// tabelę operatorów, bo ma własny tok (?:) zamiast tokenu binarnego.
func (p *Parser) parseExpr(minPrec int) (Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		if p.Cur().Type == TokQuestion && minPrec <= 1 {
			p.Next()

			thenExpr, thenErr := p.parseExpr(2)
			if thenErr != nil {
				return nil, thenErr
			}

			if p.Cur().Type != TokColon {
				left = &ConditionalExpr{Cond: left, Then: thenExpr}
				continue
			}
			p.Next()

			elseExpr, elseErr := p.parseExpr(1)
			if elseErr != nil {
				return nil, elseErr
			}
			left = &ConditionalExpr{Cond: left, Then: thenExpr, Else: elseExpr}
			continue
		}

		meta, act, ok := p.ops.ByToken(p.Cur().Type)
		if !ok || meta.Precedence < minPrec {
			return left, nil
		}
		p.Next()
		next := meta.Precedence + 1
		if meta.Associativity == Right {
			next = meta.Precedence
		}
		right, err := p.parseExpr(next)
		if err != nil {
			return nil, err
		}
		res, err := act.Do(p.ctx, OperatorReq{Left: left, Right: right, Meta: &meta})
		if err != nil {
			return nil, p.errorf(p.Cur().Line, "operator %s: %v", meta.Name, err)
		}
		left = res.Node
	}
}

// parsePrimary is the seam. Extensions get first refusal: token-based
// extensions on their token, keyword-based extensions on identifier
// tokens. Built-in primaries handle only what no extension claims:
// parenthesised groups and atoms.
func (p *Parser) parsePrimary() (Expr, error) {
	if ext, ok := p.primaries.ByToken(p.Cur().Type); ok {
		return ext.Parse(p)
	}

	if p.Cur().Type == TokIdent {
		if ext, ok := p.primaries.ByKeyword(p.Cur().Lit); ok {
			return ext.Parse(p)
		}
	}

	if p.Cur().Type == TokLParen {
		p.Next()
		e, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if p.Cur().Type != TokRParen {
			return nil, p.errorf(p.Cur().Line, "expected ')', got %q", p.Cur().Lit)
		}
		p.Next()
		return e, nil
	}

	if p.Cur().Type == TokIdent {
		return p.parseAtom()
	}

	if p.Cur().Type == TokEOF {
		return nil, p.errorf(p.Cur().Line, "expected expression, got end of input")
	}
	return nil, p.errorf(p.Cur().Line, "unexpected token %q", p.Cur().Lit)
}

// parseAtom czyta atom wraz z całą otoczką: params w nawiasach,
// modyfikatory, targets, excludes, prompt i blok @{...}.
func (p *Parser) parseAtom() (Expr, error) {
	name := p.Cur().Lit
	p.Next()
	a := &Atom{Name: name, Params: map[string]string{}}

	if p.Cur().Type == TokLParen {
		if err := p.parseLegacyParams(a); err != nil {
			return nil, err
		}
	}

	for {
		switch p.Cur().Type {
		case TokColon:
			if p.isTernaryColon() {
				return a, nil
			}
			if err := p.parseModifier(a); err != nil {
				return nil, err
			}

		case TokHash:
			p.Next()
			a.Targets = p.appendList(a.Targets)

		case TokTilde:
			p.Next()
			a.Excludes = p.appendList(a.Excludes)

		case TokAtPrompt:
			a.Prompt = p.Cur().Lit
			p.Next()

		case TokAtBrace:
			args, err := parseAtBrace(p.Cur().Lit, p.Cur().Line)
			if err != nil {
				return nil, err
			}
			if a.Args == nil {
				a.Args = args
			} else {
				maps.Copy(a.Args, args)
			}
			p.Next()

		case TokEOF, TokIdent, TokString, TokNumber, TokArrow, TokPipe,
			TokAmpersand, TokOrOr, TokLParen, TokRParen, TokLBrace,
			TokRBrace, TokEquals, TokComma, TokQuestion:
			return a, nil
		}
	}
}

func (p *Parser) parseLegacyParams(a *Atom) error {
	p.Next() // consume '('
	for p.Cur().Type != TokRParen && p.Cur().Type != TokEOF {
		if p.Cur().Type != TokIdent {
			return p.errorf(p.Cur().Line, "expected parameter name")
		}
		key := p.Cur().Lit
		p.Next()

		if p.Cur().Type != TokEquals {
			return p.errorf(p.Cur().Line, "expected '=' after %q", key)
		}
		p.Next()

		value, err := p.consumeValue()
		if err != nil {
			return err
		}
		a.Params[key] = value

		if p.Cur().Type == TokComma {
			p.Next()
		}
	}
	if p.Cur().Type != TokRParen {
		return p.errorf(p.Cur().Line, "expected ')'")
	}
	p.Next()
	return nil
}

func (p *Parser) parseModifier(a *Atom) error {
	p.Next() // consume ':'
	if p.Cur().Type != TokIdent {
		return p.errorf(p.Cur().Line, "expected modifier name")
	}
	m := p.Cur().Lit
	p.Next()

	if p.Cur().Type == TokEquals {
		p.Next()
		value, err := p.consumeValue()
		if err != nil {
			return err
		}
		m += "=" + value
	}
	a.Modifiers = append(a.Modifiers, m)
	return nil
}

// consumeValue skleja sąsiadujące tokeny ident/number/string w jedną
// wartość. Commas are included so `:role=admin,editor` parses as a
// single value for WithStringList.
func (p *Parser) consumeValue() (string, error) {
	var sb strings.Builder
	for p.valueTokenContinues() {
		p.writeValueToken(&sb)
		p.Next()
	}
	if sb.Len() == 0 {
		return "", p.errorf(p.Cur().Line, "expected value")
	}
	return sb.String(), nil
}

// valueTokenContinues reports whether the token under the cursor is
// part of the value currently being consumed. It owns the two edge
// rules: ternary `? :` breaks the value, and so does `ident =`.
func (p *Parser) valueTokenContinues() bool {
	t := p.Cur().Type

	if t == TokColon {
		if p.isTernaryColon() {
			return false
		}
		if p.pos+2 < len(p.toks) &&
			p.toks[p.pos+1].Type == TokIdent &&
			p.toks[p.pos+2].Type == TokEquals {
			return false
		}
	}

	switch t {
	case TokIdent, TokNumber, TokString,
		TokAtPrompt, TokTilde, TokHash, TokComma, TokColon:
		return true
	case TokEOF, TokArrow, TokPipe, TokAmpersand, TokOrOr,
		TokLParen, TokRParen, TokLBrace, TokRBrace,
		TokEquals, TokQuestion, TokAtBrace:
		return false
	default:
		return false
	}
}

// writeValueToken emits the current token's raw text (and any prefix
// byte for the punctuation tokens that carry one) into sb.
func (p *Parser) writeValueToken(sb *strings.Builder) {
	switch p.Cur().Type {
	case TokAtPrompt:
		sb.WriteByte('@')
	case TokTilde:
		sb.WriteByte('~')
	case TokHash:
		sb.WriteByte('#')
	case TokColon:
		sb.WriteByte(':')
	case TokEOF, TokIdent, TokString, TokNumber, TokArrow, TokPipe,
		TokAmpersand, TokOrOr, TokLParen, TokRParen, TokLBrace,
		TokRBrace, TokEquals, TokComma, TokQuestion, TokAtBrace:
		// no prefix byte; the literal is written below
	}
	sb.WriteString(p.Cur().Lit)
}

// używane przez #targets i ~excludes.
func (p *Parser) appendList(dst []string) []string {
	for {
		if p.Cur().Type != TokIdent && p.Cur().Type != TokString {
			return dst
		}
		dst = append(dst, p.Cur().Lit)
		p.Next()
		if p.Cur().Type != TokComma {
			return dst
		}
		p.Next()
	}
}
