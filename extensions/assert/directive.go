package assert

import (
	"context"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

const assertionPositionsKey = "asserts.positions"

// Directive parses post-run assertions and appends their expressions to
// meta["asserts"]. It accepts a single expression, a multiline block, or a
// compact comma-separated list.
var Directive = compiler.Directive{
	Name: "assert",
	Example: `@assert: result != nil
@assert {
  result.count > 0
  result.status == "ok"
}
@assert: { result.count > 0, result.status == "ok" }`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@assert"))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
	rest = strings.TrimSpace(compiler.StripExprComments(rest))

	if rest == "{" {
		return handleBlock(req, pos)
	}
	if wrapsInBraces(rest) {
		return handleCompact(req, pos, rest)
	}

	if rest == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@assert: expression is required")
	}
	appendAssert(req.Out, rest, pos)
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

func wrapsInBraces(source string) bool {
	if len(source) < 2 || source[0] != '{' || source[len(source)-1] != '}' {
		return false
	}
	var stack []byte
	var quote byte
	for i := 0; i < len(source); i++ {
		c := source[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(source) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '{', '[', '(':
			stack = append(stack, c)
		case '}', ']', ')':
			if len(stack) == 0 || !matchesDelimiter(stack[len(stack)-1], c) {
				return false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i == len(source)-1
			}
		}
	}
	return false
}

func matchesDelimiter(open, closing byte) bool {
	switch open {
	case '{':
		return closing == '}'
	case '[':
		return closing == ']'
	case '(':
		return closing == ')'
	default:
		return false
	}
}

// handleBlock collects each non-empty, non-comment body line as one assertion.
// The expression position is retained so runtime failures identify the line
// that supplied that assertion, not merely the directive header.
func handleBlock(req compiler.DirectiveReq, pos compiler.Position) (compiler.DirectiveRes, error) {
	added := 0
	for j := req.I + 1; j < len(req.Lines); j++ {
		current := strings.TrimSpace(compiler.StripExprComments(req.Lines[j]))
		if current == "}" {
			if added == 0 {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "@assert: block must contain at least one expression")
			}
			return compiler.DirectiveRes{Next: j + 1}, nil
		}

		expression := strings.TrimSpace(strings.TrimSuffix(current, ","))
		if expression == "" {
			continue
		}
		appendAssert(req.Out, expression, compiler.Position{File: req.File, Line: j + 1})
		added++
	}
	return compiler.DirectiveRes{}, compiler.SourceError(pos, "@assert: unclosed block, expected '}'")
}

// handleCompact splits only commas at the outer list level. The shared
// compiler splitter respects quoted strings and nested calls/collections.
func handleCompact(req compiler.DirectiveReq, pos compiler.Position, source string) (compiler.DirectiveRes, error) {
	body := strings.TrimSpace(source[1 : len(source)-1])
	if body == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@assert: block must contain at least one expression")
	}

	parts := compiler.SplitTopLevel(body, ',')
	for i, part := range parts {
		expression := strings.TrimSpace(compiler.StripExprComments(part))
		if expression == "" {
			if i == len(parts)-1 && strings.HasSuffix(strings.TrimSpace(body), ",") {
				continue
			}
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@assert: compact form contains an empty expression")
		}
		appendAssert(req.Out, expression, pos)
	}
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

func appendAssert(out map[string]any, expression string, pos compiler.Position) {
	existing, _ := out["asserts"].([]string)
	out["asserts"] = append(existing, expression)
	positions, _ := out[assertionPositionsKey].([]compiler.Position)
	out[assertionPositionsKey] = append(positions, pos)
}
