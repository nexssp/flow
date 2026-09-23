package compiler

import "strings"

// parseAtom parses an action reference with its modifiers.
//
// Grammar (informal):
//
//	atom     := ident trailer*
//	trailer  := ':' modifier
//	          | '@' prompt
//	          | '@{' args '}'
//	          | '#' target (',' target)*
//	          | '~' exclude (',' exclude)*
//	          | '(' params ')'
//	modifier := ident ('=' value)?
//	value    := ident | number | string | prompt | bracket
//	bracket  := '[' (value (',' value)*)? ']'
//
// Bracket values are new: they let a modifier carry a list, e.g.
//
//	fs.walk:dirs=[".", "src"] :ext="go,ts"
//	agent.review:targets=[.left, .right]
//
// consumeBracketContent collects the whole [...], including nested
// brackets, quoted strings, and commas. Without it the parser would stop
// at the first '[' and the trailing tokens would surface as a spurious
// "unexpected token" error.
func (p *Parser) parseAtom() (Expr, error) {
	if p.tok.Type != TokenIdent {
		return nil, p.errorf("expected identifier")
	}

	name := p.tok.Lit
	p.next()

	atom := &AtomExpr{
		Name:      name,
		Params:    make(map[string]string),
		Inputs:    make(map[string]string),
		Args:      make(map[string]*Value),
		Targets:   []string{},
		Excludes:  []string{},
		Modifiers: []string{},
	}

loop:
	for {
		switch p.tok.Type {
		case TokenColon:
			// Whitespace after ':' means the colon introduces a ternary
			// else arm, not a modifier. Stop and let parseConditional
			// consume it.
			if p.isTernaryColon() {
				break loop
			}

			p.next()
			if p.tok.Type != TokenIdent {
				return nil, p.errorf("expected identifier after ':'")
			}
			modifier := p.tok.Lit
			p.next()

			if p.tok.Type == TokenAssign {
				modifier += "="
				p.next()
				if err := p.consumeModifierValue(&modifier); err != nil {
					return nil, err
				}
			}

			atom.Modifiers = append(atom.Modifiers, modifier)
			if atom.Profile == "" && !strings.Contains(modifier, "=") {
				atom.Profile = modifier
			}

		case TokenAtPrompt:
			atom.Prompt = p.tok.Lit
			p.next()

		case TokenAtBrace:
			if err := parseAtBrace(atom, p.tok.Lit, Position{
				File: p.file,
				Line: p.tok.Line,
				Col:  p.tok.Col,
			}); err != nil {
				return nil, err
			}
			p.next()

		case TokenHash:
			p.next()
			if p.tok.Type != TokenIdent {
				return nil, p.errorf("expected target after '#'")
			}
			atom.Targets = append(atom.Targets, p.tok.Lit)
			p.next()
			for p.tok.Type == TokenComma {
				p.next()
				if p.tok.Type != TokenIdent {
					break
				}
				atom.Targets = append(atom.Targets, p.tok.Lit)
				p.next()
			}

		case TokenTilde:
			p.next()
			if p.tok.Type != TokenIdent {
				return nil, p.errorf("expected exclude after '~'")
			}
			atom.Excludes = append(atom.Excludes, p.tok.Lit)
			p.next()
			for p.tok.Type == TokenComma {
				p.next()
				if p.tok.Type != TokenIdent {
					break
				}
				atom.Excludes = append(atom.Excludes, p.tok.Lit)
				p.next()
			}

		case TokenLParen:
			if err := p.parseLegacyParams(atom); err != nil {
				return nil, err
			}

		default:
			break loop
		}
	}

	return atom, nil
}

// consumeModifierValue appends the literal text of a modifier value to
// `modifier`. It handles every token kind that can legally appear on the
// right-hand side of `:key=`. On return, `p.tok` is the first token after
// the value.
func (p *Parser) consumeModifierValue(modifier *string) error {
	for {
		switch p.tok.Type {
		case TokenIdent, TokenNumber:
			*modifier += p.tok.Lit
		case TokenString:
			// Preserve the original quoting so the downstream modifier
			// parser can distinguish `a="x:y,z"` from `a=x:y,z`.
			*modifier += `"` + p.tok.Lit + `"`
		case TokenAtPrompt:
			*modifier += "@" + p.tok.Lit
		case TokenLBracket:
			content, err := p.consumeBracketContent()
			if err != nil {
				return err
			}
			*modifier += content
			// consumeBracketContent advances past ']' itself.
			continue
		default:
			return nil
		}
		p.next()
	}
}

// consumeBracketContent consumes a balanced `[ ... ]` sequence and
// returns its literal text including the brackets. It honors quoted
// strings and nested brackets, so `["a[b]c", ["d"]]` comes back intact.
//
// Precondition: p.tok is the opening '['.
// Postcondition: p.tok is the first token after the matching ']'.
func (p *Parser) consumeBracketContent() (string, error) {
	var sb strings.Builder
	sb.WriteByte('[')
	p.next() // consume '['

	depth := 1
	for {
		switch p.tok.Type {
		case TokenEOF:
			return "", p.errorf("unclosed '[' in modifier value")

		case TokenLBracket:
			depth++
			sb.WriteByte('[')

		case TokenRBracket:
			depth--
			sb.WriteByte(']')
			if depth == 0 {
				p.next() // consume ']'
				return sb.String(), nil
			}

		case TokenString:
			sb.WriteByte('"')
			sb.WriteString(p.tok.Lit)
			sb.WriteByte('"')

		case TokenComma:
			sb.WriteByte(',')

		case TokenColon:
			sb.WriteByte(':')

		case TokenAtPrompt:
			sb.WriteByte('@')
			sb.WriteString(p.tok.Lit)

		default:
			sb.WriteString(p.tok.Lit)
		}
		p.next()
	}
}

// parseLegacyParams parses the parenthesised `(key=value, ...)` form.
// Retained for backward compatibility; new code should use `@{}` instead.
func (p *Parser) parseLegacyParams(atom *AtomExpr) error {
	p.next()
	for p.tok.Type != TokenRParen && p.tok.Type != TokenEOF {
		if p.tok.Type != TokenIdent {
			return p.errorf("expected identifier in param")
		}
		key := p.tok.Lit
		p.next()
		if !p.expect(TokenAssign) {
			return p.errorf("expected '=' after key")
		}
		if p.tok.Type != TokenIdent && p.tok.Type != TokenString && p.tok.Type != TokenNumber {
			return p.errorf("expected value")
		}
		val := p.tok.Lit
		p.next()

		if strings.Contains(val, ".") {
			atom.Inputs[key] = val
		} else {
			atom.Params[key] = val
		}

		if p.tok.Type == TokenComma {
			p.next()
		}
	}
	if !p.expect(TokenRParen) {
		return p.errorf("expected ')'")
	}
	return nil
}

// isTernaryColon reports whether the current ':' token introduces a
// ternary else arm rather than an atom modifier.
//
// The rule: whitespace between ':' and the following token. A modifier
// colon is glued to its key (`atom:timeout=30s`); a ternary colon is
// separated from its expression by whitespace (`a ? b : c`).
//
// The check walks the raw input between the two token offsets so it is
// robust against comments, escaped quotes, and every other shape the
// lexer's whitespace handling can produce.
func (p *Parser) isTernaryColon() bool {
	if p.tok.Type != TokenColon {
		return false
	}
	gapStart := p.tok.Offset + 1
	gapEnd := p.peek.Offset
	if gapEnd <= gapStart || gapEnd > len(p.l.input) {
		return false
	}
	for i := gapStart; i < gapEnd; i++ {
		switch p.l.input[i] {
		case ' ', '\t', '\r', '\n':
			// whitespace-only gap → ternary
		default:
			return false // glued → modifier
		}
	}
	return true
}
