package projection

import (
	"errors"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/internal/lexical"
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
// the opener at openIndex. Quoted strings are skipped; backslash escapes
// apply to single- and double-quoted strings, not raw backtick strings.
func scanBalancedBraces(src string, openIndex int) (int, error) {
	depth := 1
	parenDepth := 0
	i := openIndex + 1
	var quote byte

	for i < len(src) {
		c := src[i]

		if quote != 0 {
			i, quote = advanceProjectionQuote(src, i, quote)
			continue
		}
		if next, handled, err := skipBalancedBraceTrivia(src, i, parenDepth); handled {
			if err != nil {
				return -1, err
			}
			i = next
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

func skipBalancedBraceTrivia(src string, index, parenDepth int) (next int, handled bool, err error) {
	if index+1 < len(src) && src[index] == '/' {
		switch src[index+1] {
		case '/':
			for index < len(src) && src[index] != '\n' && src[index] != '\r' {
				index++
			}
			return index, true, nil
		case '*':
			end, ok := projectionBlockCommentEnd(src, index)
			if !ok {
				return -1, true, errors.New("unclosed projection")
			}
			return end, true, nil
		}
	}
	if src[index] == '#' && (parenDepth == 0 || projectionHashLineComment(src, index)) {
		for index < len(src) && src[index] != '\n' && src[index] != '\r' {
			index++
		}
		return index, true, nil
	}
	return index, false, nil
}

func projectionHashLineComment(src string, index int) bool {
	return lexical.IsHashLineComment(src, index)
}

func projectionBlockCommentEnd(src string, start int) (int, bool) {
	depth := 1
	for i := start + 2; i < len(src); i++ {
		if i+1 < len(src) && src[i:i+2] == "/*" {
			depth++
			i++
			continue
		}
		if i+1 < len(src) && src[i:i+2] == "*/" {
			depth--
			i++
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return len(src), false
}

func stripProjectionComments(raw string) string {
	out := []byte(raw)
	var quote byte
	parenDepth := 0
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
		if c == '/' && i+1 < len(raw) {
			if raw[i+1] == '/' {
				i = blankProjectionLineComment(out, i)
				continue
			}
			if raw[i+1] == '*' {
				end, ok := projectionBlockCommentEnd(raw, i)
				if !ok {
					end = len(raw)
				}
				for j := i; j < end; j++ {
					if raw[j] != '\n' && raw[j] != '\r' {
						out[j] = ' '
					}
				}
				i = end
				continue
			}
		}
		if c == '#' && (parenDepth == 0 || projectionHashLineComment(raw, i)) {
			i = blankProjectionLineComment(out, i)
			continue
		}
		switch c {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		}
		i++
	}
	return string(out)
}

func blankProjectionLineComment(out []byte, start int) int {
	position := start
	for position < len(out) && out[position] != '\n' && out[position] != '\r' {
		out[position] = ' '
		position++
	}
	return position
}
