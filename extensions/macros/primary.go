package macros

import (
	"strings"

	"github.com/nexssp/flow/core"
)

// macroPrimary dispatches on TokAtPrompt. The parser hands every
// @name token to Parse; if the name matches a declared macro, the
// body is substituted and re-parsed in the caller's context.
type macroPrimary struct {
	byName map[string]Declaration
}

func (m *macroPrimary) Name() string { return "macros.expand" }

func (m *macroPrimary) TokenType() core.TokenType { return core.TokAtPrompt }

func (m *macroPrimary) Parse(p *core.Parser) (core.Expr, error) {
	nameTok := p.Current()
	if nameTok.Type != core.TokAtPrompt {
		return nil, p.Fail(nameTok.Line, "internal: macros.expand called on %s", nameTok.Type)
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
	return p.SubParse(body)
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
