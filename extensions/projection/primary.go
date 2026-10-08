package projection

import (
	"errors"
	"strings"

	"github.com/nexssp/flow/core"
)

// Extension is the compile-time parser extension. It
// dispatches on the opening brace and scans the raw source up to the
// matching close, tracking quotes so braces inside strings are
// ignored.
type Extension struct{}

func (Extension) Name() string              { return "projection" }
func (Extension) TokenType() core.TokenType { return core.TokLBrace }

func (Extension) Parse(p *core.Parser) (core.Expr, error) {
	open := p.Cur()
	if open.Type != core.TokLBrace {
		return nil, p.Fail(open.Line, "projection: expected '{'")
	}
	src := p.Src()
	if open.Offset >= len(src) || src[open.Offset] != '{' {
		return nil, p.Fail(open.Line, "projection: offset out of range")
	}

	closeIndex, err := scanBalancedBraces(src, open.Offset)
	if err != nil {
		return nil, p.Fail(open.Line, "%v", err)
	}

	raw := stripHashComments(src[open.Offset+1 : closeIndex])

	for p.Cur().Type != core.TokEOF {
		if p.Cur().Offset >= closeIndex {
			if p.Cur().Offset == closeIndex && p.Cur().Type == core.TokRBrace {
				p.Next()
			}
			break
		}
		p.Next()
	}

	return &core.ProjectionExpr{
		Raw: raw,
		Pos: core.Position{File: p.File(), Line: open.Line, Col: open.Offset - strings.LastIndexByte(p.Src()[:open.Offset], '\n')},
	}, nil
}

// stripHashComments removes `#`-to-EOL comments from a projection body.
//
// The rule is depth-aware: `#` starts a comment only at parenthesis
// depth 0. Inside parentheses `#` is expr-lang's iterator placeholder
// (`filter(# > 0)`, `map(#.name)`, `sortBy(#.line, "desc")`) and must be
// preserved verbatim, or the projection fails with
// "unexpected token Bracket" / "unclosed projection".
//
// Line terminators (\n or \r\n) are preserved so expr-lang line numbers
// still match the source file — wrapMacroError and the on_error wrapper
// remap positions using that alignment.
func stripHashComments(raw string) string {
	if !strings.Contains(raw, "#") {
		return raw
	}
	var sb strings.Builder
	sb.Grow(len(raw))

	var quote byte
	depth := 0
	i := 0
	for i < len(raw) {
		c := raw[i]

		if quote != 0 {
			sb.WriteByte(c)
			if c == '\\' && i+1 < len(raw) {
				sb.WriteByte(raw[i+1])
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}

		switch c {
		case '"', '\'', '`':
			quote = c
			sb.WriteByte(c)
		case '(':
			depth++
			sb.WriteByte(c)
		case ')':
			if depth > 0 {
				depth--
			}
			sb.WriteByte(c)
		case '#':
			if depth > 0 {
				// expr-lang iterator placeholder inside filter/map/etc.
				sb.WriteByte(c)
				i++
				continue
			}
			// Comment: skip to the next line terminator, then write the
			// terminator verbatim so CRLF files stay CRLF.
			for i < len(raw) && raw[i] != '\n' && raw[i] != '\r' {
				i++
			}
			for i < len(raw) && (raw[i] == '\r' || raw[i] == '\n') {
				sb.WriteByte(raw[i])
				i++
			}
			continue
		default:
			sb.WriteByte(c)
		}
		i++
	}
	return sb.String()
}

// scanBalancedBraces returns the index of the closing '}' that matches
// the opener at openIndex. Quoted strings are skipped; escape
// sequences inside them are honored.
func scanBalancedBraces(src string, openIndex int) (int, error) {
	depth := 1
	parenDepth := 0
	i := openIndex + 1
	var quote byte

	for i < len(src) {
		c := src[i]

		if quote != 0 {
			if c == '\\' && i+1 < len(src) {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}

		switch c {
		case '"', '\'', '`':
			quote = c
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '#':
			if parenDepth > 0 {
				// Iterator placeholder; not a comment.
				i++
				continue
			}
			// Depth-0 comment: `}` inside it must not close the
			// projection, so skip to end of line.
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				i++
			}
			continue
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
		i++
	}
	return -1, errors.New("unclosed projection")
}
