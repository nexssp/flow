package compiler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const maxParseDepth = 64

type Parser struct {
	ctx           context.Context
	file          string
	lineBase      int
	src           string
	toks          []Token
	pos           int
	ops           *OperatorTable
	primaries     *PrimaryExtensionTable
	depth         int
	lineModifiers []LineLookup
}

type LineLookup struct {
	Source  ModifierSource
	Fn      func(line int) []string
	ValueFn func(line int) []ModifierValue
}

func NewParser(ctx context.Context, ops *OperatorTable, src string) *Parser {
	return NewParserWithFileOffset(ctx, ops, DefaultPrimaryExtensions(), src, "", 0)
}

func NewParserWithPrimaries(
	ctx context.Context,
	ops *OperatorTable,
	primaries *PrimaryExtensionTable,
	src string,
) *Parser {
	return NewParserWithFileOffset(ctx, ops, primaries, src, "", 0)
}

func NewParserWithFile(
	ctx context.Context,
	ops *OperatorTable,
	primaries *PrimaryExtensionTable,
	src, file string,
) *Parser {
	return NewParserWithFileOffset(ctx, ops, primaries, src, file, 0)
}

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

func (p *Parser) Cur() Token { return p.toks[p.pos] }

func (p *Parser) Current() Token { return p.toks[p.pos] }

func (p *Parser) Peek(offset int) Token {
	index := max(p.pos+offset, 0)
	if index >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[index]
}

func (p *Parser) Next() {
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
}

func (p *Parser) Advance() { p.Next() }

func (p *Parser) errorf(_ int, format string, args ...any) error {
	return SourceError(p.currentPos(), format, args...)
}

func (p *Parser) currentPos() Position {
	tok := p.Cur()
	col := 0
	if tok.Offset >= 0 && tok.Offset <= len(p.src) {
		lastNL := strings.LastIndexByte(p.src[:tok.Offset], '\n')
		col = tok.Offset - lastNL
	}
	return Position{File: p.file, Line: tok.Line + p.lineBase, Col: col}
}

func (p *Parser) Position() Position { return p.currentPos() }

func (p *Parser) Fail(_ int, format string, args ...any) error {
	return p.errorf(0, format, args...)
}

func (p *Parser) Src() string { return p.src }

func (p *Parser) Context() context.Context { return p.ctx }

func (p *Parser) File() string { return p.file }

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

func (p *Parser) ParseExpr(minPrec int) (Expr, error) {
	return p.parseExpr(minPrec)
}

func (p *Parser) SubParse(src string) (Expr, error) {
	if p.depth >= maxParseDepth {
		return nil, fmt.Errorf("core: parse depth exceeded (%d)", maxParseDepth)
	}
	sub := NewParserWithFileOffset(p.ctx, p.ops, p.primaries, src, p.file, p.lineBase)
	sub.depth = p.depth + 1
	sub.lineModifiers = p.lineModifiers
	return sub.Parse()
}

func (p *Parser) parseExpr(minPrec int) (Expr, error) {
	if p.depth >= maxParseDepth {
		return nil, p.errorf(p.Cur().Line, "parse depth exceeded (%d)", maxParseDepth)
	}
	p.depth++
	defer func() { p.depth-- }()

	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		if p.Cur().Type == TokQuestion && minPrec <= 1 {
			if ext, ok := p.primaries.ByPostfix(TokQuestion); ok && ext.MatchesPostfix(p) {
				left, err = ext.ParsePostfix(p, left)
				if err != nil {
					return nil, err
				}
				continue
			}

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
		if res.Node == nil {
			return nil, p.errorf(p.Cur().Line, "operator %s returned a nil expression", meta.Name)
		}
		left = res.Node
	}
}

func (p *Parser) parsePrimary() (Expr, error) {
	startPos := p.currentPos()
	startLine := p.Cur().Line
	_, isExtensionPrimary := p.primaries.ByToken(p.Cur().Type)
	if !isExtensionPrimary && p.Cur().Type == TokIdent {
		_, isExtensionPrimary = p.primaries.ByKeyword(p.Cur().Lit)
	}

	expr, err := p.parsePrimaryInner()
	if err != nil {
		return nil, err
	}
	if expr == nil {
		return nil, p.errorf(p.Cur().Line, "primary extension returned a nil expression")
	}
	expr, err = p.parsePostfixModifiers(expr)
	if err != nil {
		return nil, err
	}
	if expr == nil {
		return nil, p.errorf(p.Cur().Line, "postfix extension returned a nil expression")
	}
	if isExtensionPrimary {
		expr = p.prependInheritedToExpr(expr, startLine, startPos)
	} else if _, isAtom := expr.(*Atom); isAtom {
		expr = p.prependInheritedToExpr(expr, startLine, startPos)
	}
	return expr, nil
}

func (p *Parser) parsePrimaryInner() (Expr, error) {
	if ext, ok := p.primaries.ByToken(p.Cur().Type); ok {
		return ext.Parse(p)
	}

	if p.Cur().Type == TokIdent {
		switch p.Cur().Lit {
		case "true":
			pos := p.currentPos()
			p.Next()
			return &LiteralExpr{Val: true, Pos: pos}, nil
		case "false":
			pos := p.currentPos()
			p.Next()
			return &LiteralExpr{Val: false, Pos: pos}, nil
		case "null":
			pos := p.currentPos()
			p.Next()
			return &LiteralExpr{Val: nil, Pos: pos}, nil
		}
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

	if p.Cur().Type == TokString {
		if p.Cur().Unterminated {
			return nil, p.errorf(p.Cur().Line, "unterminated string")
		}
		s := p.Cur().Lit
		pos := p.currentPos()
		p.Next()
		return &LiteralExpr{Val: s, Pos: pos}, nil
	}

	if p.Cur().Type == TokNumber {
		raw := p.Cur().Lit
		pos := p.currentPos()
		p.Next()
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return &LiteralExpr{Val: n, Pos: pos}, nil
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return &LiteralExpr{Val: f, Pos: pos}, nil
		}
		return &LiteralExpr{Val: raw, Pos: pos}, nil
	}

	if p.Cur().Type == TokIdent {
		return p.parseAtom()
	}

	if p.Cur().Type == TokEOF {
		return nil, p.errorf(p.Cur().Line, "expected expression, got end of input")
	}
	return nil, p.errorf(p.Cur().Line, "unexpected token %q", p.Cur().Lit)
}

func (p *Parser) parsePostfixModifiers(expr Expr) (Expr, error) {
	if p.Cur().Type != TokColon || p.isTernaryColon() {
		return expr, nil
	}
	if _, isAtom := expr.(*Atom); isAtom {
		return expr, nil
	}

	startPos := p.Position()
	var mods []string
	var values []ModifierValue
	for p.Cur().Type == TokColon && !p.isTernaryColon() {
		p.Next()
		if p.Cur().Type != TokIdent {
			return nil, p.errorf(p.Cur().Line, "expected modifier name")
		}
		name := p.Cur().Lit
		p.Next()
		if p.Cur().Type == TokEquals {
			p.Next()
			quoted := p.Cur().Type == TokString
			value, err := p.consumeValue()
			if err != nil {
				return nil, err
			}
			name += "=" + value
			values = append(values, ModifierValue{Raw: name, Quoted: quoted})
		} else {
			values = append(values, ModifierValue{Raw: name})
		}
		mods = append(mods, name)
	}
	return &DecoratedExpr{Inner: expr, Modifiers: mods, ModifierValues: values, Pos: startPos}, nil
}

func (p *Parser) parseAtomInner() (Expr, error) {
	name := p.Cur().Lit
	p.Next()
	if canonical, ok := TranslateKeyword(name); ok {
		name = canonical
	}
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
			prompt := strings.TrimSpace(p.Cur().Lit)
			if prompt == "config" {
				a.ConfigInject = true
			} else {
				a.Prompt = prompt
			}
			p.Next()

		case TokAtBrace:
			if p.Cur().Unterminated {
				return nil, p.errorf(p.Cur().Line, "unterminated argument block; expected '}'")
			}
			args, err := parseAtBrace(p.Cur().Lit, p.Position())
			if err != nil {
				return nil, err
			}
			if a.Args == nil {
				a.Args = args
			} else {
				for key, value := range args {
					if _, exists := a.Args[key]; exists {
						return nil, SourceError(p.Position(), "duplicate argument %q", key)
					}
					a.Args[key] = value
				}
			}
			p.Next()

		case TokEOF, TokIdent, TokString, TokNumber, TokArrow, TokPipe,
			TokAmpersand, TokOrOr, TokLParen, TokRParen, TokLBrace,
			TokRBrace, TokEquals, TokComma, TokQuestion:
			return a, nil
		}
	}
}

func (p *Parser) parseAtom() (Expr, error) {
	pos := p.currentPos()
	expr, err := p.parseAtomInner()
	if err != nil {
		return nil, err
	}
	a, ok := expr.(*Atom)
	if !ok {
		return expr, nil
	}
	a.Pos = pos
	// Sourced modifiers start with those directly placed on the atom.
	a.ModifierSources = make([]ModifierSource, len(a.Modifiers))
	for i := range a.ModifierSources {
		a.ModifierSources[i] = ModifierSource{Kind: "atom"}
	}
	for len(a.ModifierValues) < len(a.Modifiers) {
		i := len(a.ModifierValues)
		a.ModifierValues = append(a.ModifierValues, ModifierValue{Raw: a.Modifiers[i]})
	}
	return expr, nil
}

func (p *Parser) prependInherited(a *Atom, line int) {
	inherited := p.inheritedForLine(line, a.Modifiers)
	if len(inherited) == 0 {
		return
	}
	appliedRaw := make([]string, 0, len(inherited))
	appliedSrc := make([]ModifierSource, 0, len(inherited))
	appliedValues := make([]ModifierValue, 0, len(inherited))
	for _, modifier := range inherited {
		appliedRaw = append(appliedRaw, modifier.raw)
		appliedSrc = append(appliedSrc, modifier.source)
		appliedValues = append(appliedValues, modifier.value)
	}
	a.Modifiers = append(appliedRaw, a.Modifiers...)
	a.ModifierValues = append(appliedValues, a.ModifierValues...)
	a.ModifierSources = append(appliedSrc, a.ModifierSources...)
}

func (p *Parser) prependInheritedToExpr(expr Expr, line int, pos Position) Expr {
	switch node := expr.(type) {
	case *Atom:
		p.prependInherited(node, line)
		return node
	case *DecoratedExpr:
		inherited := p.inheritedForLine(line, node.Modifiers)
		if len(inherited) == 0 {
			return node
		}
		applied := make([]string, 0, len(inherited))
		appliedValues := make([]ModifierValue, 0, len(inherited))
		for _, modifier := range inherited {
			applied = append(applied, modifier.raw)
			appliedValues = append(appliedValues, modifier.value)
		}
		node.Modifiers = append(applied, node.Modifiers...)
		node.ModifierValues = append(appliedValues, node.ModifierValues...)
		return node
	default:
		inherited := p.inheritedForLine(line, nil)
		if len(inherited) == 0 {
			return expr
		}
		modifiers := make([]string, len(inherited))
		values := make([]ModifierValue, len(inherited))
		for i, modifier := range inherited {
			modifiers[i] = modifier.raw
			values[i] = modifier.value
		}
		return &DecoratedExpr{Inner: expr, Modifiers: modifiers, ModifierValues: values, Pos: pos}
	}
}

type sourcedLineModifier struct {
	raw    string
	source ModifierSource
	value  ModifierValue
}

func (p *Parser) inheritedForLine(line int, localModifiers []string) []sourcedLineModifier {
	if len(p.lineModifiers) == 0 {
		return nil
	}

	localNames := make(map[string]bool, len(localModifiers))
	for _, raw := range localModifiers {
		localNames[ModifierName(raw)] = true
	}

	index := make(map[string]int)
	var applied []sourcedLineModifier
	for _, lookup := range p.lineModifiers {
		if lookup.Fn == nil && lookup.ValueFn == nil {
			continue
		}
		values := make([]ModifierValue, 0)
		if lookup.ValueFn != nil {
			values = append(values, lookup.ValueFn(line)...)
		} else {
			for _, raw := range lookup.Fn(line) {
				values = append(values, ModifierValue{Raw: raw})
			}
		}
		for _, value := range values {
			raw := value.Raw
			name := ModifierName(raw)
			if localNames[name] {
				continue
			}
			modifier := sourcedLineModifier{raw: raw, source: lookup.Source, value: value}
			if i, ok := index[name]; ok {
				applied[i] = modifier
			} else {
				index[name] = len(applied)
				applied = append(applied, modifier)
			}
		}
	}
	return applied
}

func (p *Parser) parseLegacyParams(a *Atom) error {
	p.Next() // consume '('
	for p.Cur().Type != TokRParen && p.Cur().Type != TokEOF {
		if p.Cur().Type != TokIdent {
			return p.errorf(p.Cur().Line, "expected parameter name")
		}
		key := p.Cur().Lit
		p.Next()
		if _, exists := a.Params[key]; exists {
			return p.errorf(p.Cur().Line, "duplicate parameter %q", key)
		}

		if p.Cur().Type != TokEquals {
			return p.errorf(p.Cur().Line, "expected '=' after %q", key)
		}
		p.Next()

		value, err := p.consumeLegacyValue()
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
	quoted := false

	if p.Cur().Type == TokEquals {
		p.Next()
		quoted = p.Cur().Type == TokString
		value, err := p.consumeValue()
		if err != nil {
			return err
		}
		m += "=" + value
	}
	a.Modifiers = append(a.Modifiers, m)
	a.ModifierValues = append(a.ModifierValues, ModifierValue{Raw: m, Quoted: quoted})
	return nil
}

func (p *Parser) consumeValue() (string, error) {
	var sb strings.Builder
	for p.valueTokenContinues() {
		if p.Cur().Type == TokString && p.Cur().Unterminated {
			return "", p.errorf(p.Cur().Line, "unterminated string")
		}
		p.writeValueToken(&sb)
		p.Next()
	}
	if sb.Len() == 0 {
		return "", p.errorf(p.Cur().Line, "expected value")
	}
	return sb.String(), nil
}

func (p *Parser) consumeLegacyValue() (string, error) {
	var sb strings.Builder
	for p.Cur().Type != TokComma && p.valueTokenContinues() {
		if p.Cur().Type == TokString && p.Cur().Unterminated {
			return "", p.errorf(p.Cur().Line, "unterminated string")
		}
		p.writeValueToken(&sb)
		p.Next()
	}
	if sb.Len() == 0 {
		return "", p.errorf(p.Cur().Line, "expected value")
	}
	return sb.String(), nil
}

func (p *Parser) valueTokenContinues() bool {
	t := p.Cur().Type

	if t == TokColon {
		if p.isTernaryColon() {
			return false
		}
		// Both :flag and :name=value begin a new modifier. Quoted
		// colons remain inside a TokString; ternary separators stop above.
		if p.pos+1 < len(p.toks) && p.toks[p.pos+1].Type == TokIdent {
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
		// Other tokens write only their literal value.
	}
	sb.WriteString(p.Cur().Lit)
}

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

func (p *Parser) WithLineModifiers(lookups ...LineLookup) *Parser {
	p.lineModifiers = lookups
	return p
}
