package macros

import (
	"strconv"
	"strings"

	"github.com/nexssp/flow/core"
)

const (
	// maxMacroDepth bounds nested macro expansion depth. Sixteen is
	// generous for any reasonable macro; nested macros in practice go
	// two or three deep. The guard lives here, not in core: expansion
	// depth is a macro semantic, not a parser semantic.
	maxMacroDepth = 16
)

// macroPrimary dispatches on TokAtPrompt. The parser hands every
// @name token to Parse; if the name matches a declared macro, the
// body is substituted and re-parsed in the caller's context.
type macroPrimary struct {
	byName map[string]Declaration

	// depth tracks the current expansion nesting depth. Every nested
	// call to Parse goes through the same macroPrimary instance (the
	// sub-parser created by SubParse shares the primaries table), so a
	// simple counter with increment/decrement via defer is sufficient.
	// Reset to zero at the top of every successful expansion; a parse
	// error unwinds the counter through deferred decrements.
	depth int
}

func (m *macroPrimary) Name() string { return "macros.expand" }

func (m *macroPrimary) TokenType() core.TokenType { return core.TokAtPrompt }

func (m *macroPrimary) Parse(p *core.Parser) (core.Expr, error) {
	nameTok := p.Current()
	if nameTok.Type != core.TokAtPrompt {
		return nil, p.Fail(nameTok.Line, "internal: macros.expand called on %s", nameTok.Type)
	}
	invokeLine := nameTok.Line

	if m.depth >= maxMacroDepth {
		return nil, p.Fail(nameTok.Line,
			"macro @%s: expansion depth exceeded (%d)", nameTok.Lit, maxMacroDepth)
	}

	declaration, ok := m.byName[nameTok.Lit]
	if !ok {
		// User-declared macros take precedence; the built-ins are only a
		// fallback so that a flow can rely on `@check_required(...)` etc.
		// without having to redeclare them, while a user macro with the
		// same name still shadows the built-in.
		for _, builtin := range DefaultBuiltinMacros {
			if builtin.Name == nameTok.Lit {
				declaration, ok = builtin, true
				break
			}
		}
	}
	if !ok {
		return nil, p.Fail(nameTok.Line, "unknown macro @%s", nameTok.Lit)
	}

	p.Advance() // consume @name

	args, err := readArgs(p)
	if err != nil {
		return nil, err
	}

	body := substituteParams(declaration.Body, declaration.Params, args)

	m.depth++
	defer func() { m.depth-- }()

	expr, err := p.SubParse(body)
	if err != nil {
		return nil, wrapMacroError(err, p, nameTok.Lit, invokeLine, declaration)
	}
	return expr, nil
}

// wrapMacroError attaches macro context — the name and the definition
// line — to an error raised while parsing the expanded body.
//
// When this is the innermost wrap, the body-relative line reported by
// the sub-parse is remapped to the real file line using decl.BodyLine.
// The resulting error's outer position is that file line, so modern
// terminals hyperlink directly to the failing token.
//
// Outer wraps of the same error see "in macro @" in the message and
// add their own macro context without re-remapping; each layer
// contributes one definition line to the chain.
func wrapMacroError(err error, p *core.Parser, name string, invokeLine int, decl Declaration) error {
	file := p.File()

	// Already wrapped by an inner macro: the inner remap has already
	// converted the position to file-relative. Prefix this macro's
	// context at the invocation line.
	if strings.Contains(err.Error(), "in macro @") {
		return core.SourceError(
			core.Position{File: file, Line: invokeLine},
			"in macro @%s (defined at %s:%d): %s",
			name, file, decl.DefLine, err.Error())
	}

	// First (innermost) wrap: remap the body-relative line to a
	// file-relative one so the error's outer position is clickable.
	bodyLine := parseBodyLine(err, file)
	if bodyLine <= 0 {
		return core.SourceError(
			core.Position{File: file, Line: invokeLine},
			"in macro @%s (defined at %s:%d): %s",
			name, file, decl.DefLine, err.Error())
	}
	actualLine := decl.BodyLine + bodyLine - 1
	cleanMsg := stripFileLinePrefix(err, file)
	return core.SourceError(
		core.Position{File: file, Line: actualLine},
		"in macro @%s (defined at %s:%d): %s",
		name, file, decl.DefLine, cleanMsg)
}

// parseBodyLine extracts the line number from an error of the form
// `file:line: message`. Returns 0 when the message does not carry that
// prefix, which happens for handler-raised errors that were not
// positioned via SourceError.
func parseBodyLine(err error, file string) int {
	msg := err.Error()
	rest, ok := strings.CutPrefix(msg, file+":")
	if !ok {
		return 0
	}
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		return 0
	}
	n, convErr := strconv.Atoi(rest[:colon])
	if convErr != nil {
		return 0
	}
	return n
}

// stripFileLinePrefix removes the `file:line:` prefix from a message.
// The position is carried by the wrapping SourceError instead, so the
// message body reads cleanly without a duplicated location.
func stripFileLinePrefix(err error, file string) string {
	msg := err.Error()
	rest, ok := strings.CutPrefix(msg, file+":")
	if !ok {
		return msg
	}
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		return msg
	}
	return strings.TrimLeft(rest[colon+1:], " ")
}

// readArgs consumes an optional `(arg1, arg2, ...)` group, slicing the
// raw text between the outer parens and splitting it with the same
// top-level splitter the directive parsers use. Commas inside quotes
// or nested brackets are preserved.
func readArgs(p *core.Parser) ([]string, error) {
	if p.Current().Type != core.TokLParen {
		return nil, nil
	}

	openOffset := p.Current().Offset
	p.Advance() // consume '('

	src := p.Src()
	depth := 1
	endOffset := -1

	for p.Current().Type != core.TokEOF {
		switch p.Current().Type {
		case core.TokLParen:
			depth++
		case core.TokRParen:
			depth--
			if depth == 0 {
				endOffset = p.Current().Offset
			}
		case core.TokEOF, core.TokIdent, core.TokString, core.TokNumber,
			core.TokArrow, core.TokPipe, core.TokAmpersand, core.TokOrOr,
			core.TokLBrace, core.TokRBrace, core.TokColon, core.TokEquals,
			core.TokComma, core.TokQuestion, core.TokHash, core.TokTilde,
			core.TokAtBrace, core.TokAtPrompt:
		}
		if endOffset >= 0 {
			break
		}
		p.Advance()
	}

	if endOffset < 0 {
		return nil, p.Fail(p.Current().Line, "macros: unclosed '(' in macro call")
	}

	inner := strings.TrimSpace(src[openOffset+1 : endOffset])
	p.Advance() // consume ')'

	if inner == "" {
		return nil, nil
	}

	parts := core.SplitTopLevel(inner, ',')
	out := make([]string, 0, len(parts))
	for _, a := range parts {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

// substituteParams replaces $name placeholders with the caller's
// arguments. Missing arguments become the empty string. Arguments are
// passed verbatim (quotes included), so a macro body must not wrap
// $param in its own quotes: write `value: $v`, not `value: "$v"`.
func substituteParams(body string, params, args []string) string {
	out := body
	for i, param := range params {
		arg := ""
		if i < len(args) {
			arg = args[i]
		}
		out = strings.ReplaceAll(out, "$"+param, arg)
	}
	return out
}
