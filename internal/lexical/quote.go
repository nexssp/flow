package lexical

import (
	"errors"
	"strings"
	"unicode/utf8"

	exprast "github.com/expr-lang/expr/ast"
	exprparser "github.com/expr-lang/expr/parser"
)

// CanStartQuote reports whether a quote at position begins a quoted value
// within the source region starting at start.
func CanStartQuote(source string, position, start int) bool {
	for i := position - 1; i >= start; i-- {
		char := source[i]
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' {
			continue
		}
		return char == ':' || char == ',' || char == '[' || char == '{'
	}
	return true
}

// BackslashEscapesNext reports whether the current backslash consumes the
// following byte in a quoted string. Backtick strings are raw.
func BackslashEscapesNext(quote, current byte, hasNext bool) bool {
	return quote != '`' && current == '\\' && hasNext
}

// DecodeDoubleQuotedString decodes a complete double-quoted literal using the
// same grammar as expr-lang. Plain, valid literals return a source substring
// without allocation; escaped or unusual source is parsed by expr-lang.
func DecodeDoubleQuotedString(source string) (string, error) {
	if len(source) < 2 || source[0] != '"' || source[len(source)-1] != '"' {
		return "", errors.New("expected a complete double-quoted string")
	}

	inner := source[1 : len(source)-1]
	if !strings.ContainsAny(inner, "\\\r\n\"") && utf8.ValidString(inner) {
		return inner, nil
	}

	tree, err := exprparser.Parse(source)
	if err != nil {
		return "", err
	}
	literal, ok := tree.Node.(*exprast.StringNode)
	if !ok {
		return "", errors.New("expected a double-quoted string literal")
	}
	return literal.Value, nil
}
