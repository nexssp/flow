package macros

import (
	"context"
	"errors"
	"strings"

	"github.com/nexssp/flow/core"
)

// Directive parses `@macro name(p1, p2) { body }` into a Declaration
// appended to meta[DeclarationKey]. Duplicate names are a compile
// error, not a silent override.
var Directive = core.Directive{
	Name:    "macro",
	Example: "@macro greet(name) {\n  runtime.const @{ value: $name }\n}",
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}

	// Header parsing uses the trimmed line; body reading uses the raw
	// line so byte offsets into it stay valid.
	trimmed := strings.TrimSpace(req.Lines[req.I])
	raw := req.Lines[req.I]

	openBrace := strings.IndexByte(raw, '{')
	if openBrace < 0 {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@macro: expected `name(params) { body }`")
	}

	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "@macro"))
	openParen := strings.IndexByte(rest, '(')
	closeParen := strings.IndexByte(rest, ')')
	if openParen < 0 || closeParen < 0 {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@macro: expected `name(params) { body }`")
	}

	name := strings.TrimSpace(rest[:openParen])
	if name == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@macro: name is required")
	}

	params := splitParams(rest[openParen+1 : closeParen])

	body, next, err := readBody(req.Lines, req.I, openBrace)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos, "@macro %s: %v", name, err)
	}

	existing, _ := req.Out[DeclarationKey].([]Declaration)
	for _, prior := range existing {
		if prior.Name == name {
			return core.DirectiveRes{}, core.SourceError(pos,
				"@macro %s: duplicate declaration", name)
		}
	}

	req.Out[DeclarationKey] = append(existing, Declaration{
		Name: name, Params: params, Body: body,
	})
	return core.DirectiveRes{Next: next}, nil
}

func splitParams(raw string) []string {
	var out []string
	for p := range strings.SplitSeq(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// readBody reads the body of a `@macro ... { ... }` declaration.
//
// Inline form on one line:  `@macro x { runtime.const @{ value: "hi" } }`
// Multi-line form:          opening brace ends the header line, body
//
//	spans until a line whose only content is
//	`}`.
//
// The terminator must be a line of its own. A `}` that appears on a
// body line usually closes a nested construct like `@{ ... }` and must
// not be treated as the macro terminator.
func readBody(lines []string, start, openBrace int) (body string, next int, err error) {
	line := lines[start]
	after := line[openBrace+1:]

	if closeIdx := strings.LastIndexByte(after, '}'); closeIdx >= 0 {
		if strings.TrimSpace(after[closeIdx+1:]) == "" {
			return strings.TrimSpace(after[:closeIdx]), start + 1, nil
		}
	}

	var parts []string
	if t := strings.TrimSpace(after); t != "" {
		parts = append(parts, t)
	}

	for j := start + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "}" {
			return strings.TrimSpace(strings.Join(parts, "\n")), j + 1, nil
		}
		parts = append(parts, lines[j])
	}
	return "", 0, errors.New("unclosed '{'")
}
