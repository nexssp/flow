package projection

import (
	"errors"
	"fmt"
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
		return nil, fmt.Errorf("line %d: expected '{'", open.Line)
	}
	src := p.Src()
	if open.Offset >= len(src) || src[open.Offset] != '{' {
		return nil, fmt.Errorf("line %d: projection offset out of range", open.Line)
	}

	closeIndex, err := scanBalancedBraces(src, open.Offset)
	if err != nil {
		return nil, fmt.Errorf("line %d: %w", open.Line, err)
	}

	raw := strings.TrimSpace(src[open.Offset+1 : closeIndex])

	for p.Cur().Type != core.TokEOF {
		if p.Cur().Offset >= closeIndex {
			if p.Cur().Offset == closeIndex && p.Cur().Type == core.TokRBrace {
				p.Next()
			}
			break
		}
		p.Next()
	}

	return &core.ProjectionExpr{Raw: raw}, nil
}

// scanBalancedBraces returns the index of the closing '}' that matches
// the opener at openIndex. Quoted strings are skipped; escape
// sequences inside them are honored.
func scanBalancedBraces(src string, openIndex int) (int, error) {
	depth := 1
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
