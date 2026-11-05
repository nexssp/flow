package projection

import (
	"errors"

	compiler "github.com/nexssp/flow/compiler"
)

// Extension is the compile-time parser extension. It
// dispatches on the opening brace and scans the raw source up to the
// matching close, tracking quotes so braces inside strings are
// ignored.
type Extension struct {
	functions *FunctionSet
}

// MergeWith carries a source-local function set into the existing projection
// parser. The compiler merges it only into compilations that declared or
// inherited those functions.
func (e Extension) MergeWith(older compiler.PrimaryExtension) compiler.PrimaryExtension {
	previous, ok := older.(Extension)
	if !ok {
		return e
	}
	return Extension{functions: MergeFunctionSets(previous.functions, e.functions)}
}

func (Extension) Name() string                  { return "projection" }
func (Extension) TokenType() compiler.TokenType { return compiler.TokLBrace }

func (e Extension) Parse(p *compiler.Parser) (compiler.Expr, error) {
	open := p.Cur()
	if open.Type != compiler.TokLBrace {
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

	return parseProjectionBody(p, closeIndex, p.Position(), e.functions)
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
