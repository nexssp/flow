package macros

import (
	"context"
	"errors"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

const (
	// maxMacroBodyBytes bounds the source length of a single macro
	// declaration. Guards against a well-formed but pathological
	// declaration that would make substitution and re-parsing expensive.
	maxMacroBodyBytes = 32 * 1024
)

// Directive parses `@macro name(p1, p2) { body }` into a Declaration
// appended to meta[DeclarationKey]. Duplicate names are a compile
// error, not a silent override.
//
// Every declaration is also checked against two static limits at
// declaration time — body size and recursion — so a bad macro is
// rejected before any invocation. Expansion depth is bounded at parse
// time by macroPrimary, not here: it depends on invocation order, not
// on the declaration order.
var Directive = compiler.Directive{
	Name:        "macro",
	Example:     "@macro greet(name) {\n  runtime.const @{ value: $name }\n}",
	Handler:     handleDirective,
	OpaqueBlock: true,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}

	// Header parsing uses the trimmed line; body reading uses the raw
	// line so byte offsets into it stay valid.
	trimmed := strings.TrimSpace(req.Lines[req.I])
	raw := req.Lines[req.I]

	openBrace := strings.IndexByte(raw, '{')
	if openBrace < 0 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@macro: expected `name(params) { body }`")
	}

	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "@macro"))
	openParen := strings.IndexByte(rest, '(')
	closeParen := strings.IndexByte(rest, ')')
	if openParen < 0 || closeParen < 0 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@macro: expected `name(params) { body }`")
	}

	name := strings.TrimSpace(rest[:openParen])
	if !validMacroName(name) {
		if name != "" {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@macro: invalid name %q", name)
		}
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@macro: name is required")
	}

	params, paramKinds, err := parseParams(rest[openParen+1:closeParen], pos, name)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}

	body, bodyLine, next, err := readBody(req.Lines, req.I, openBrace)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@macro %s: %v", name, err)
	}

	existing, _ := req.Out[DeclarationKey].([]Declaration)
	if err := req.Declare("macro", name); err != nil {
		return compiler.DirectiveRes{}, err
	}

	decl := Declaration{
		Name:       name,
		Params:     params,
		ParamKinds: paramKinds,
		Body:       body,
		DefFile:    req.File,
		DefLine:    pos.Line,
		BodyLine:   bodyLine,
	}
	if err := validateMacroDeclaration(decl, existing, pos); err != nil {
		return compiler.DirectiveRes{}, err
	}

	req.Out[DeclarationKey] = append(existing, decl)
	return compiler.DirectiveRes{Next: next}, nil
}

// validateMacroDeclaration rejects a new macro declaration that would
// exceed the body size limit or close a recursion cycle. Both checks
// run at declaration time so a bad macro is rejected before any
// invocation.
//
// Expansion depth is not checked here: it depends on invocation order,
// not on declaration order. macroPrimary enforces it at parse time.
func validateMacroDeclaration(decl Declaration, existing []Declaration, pos compiler.Position) error {
	if len(decl.Body) > maxMacroBodyBytes {
		return compiler.SourceError(pos,
			"@macro %s: body is %d bytes (limit %d)",
			decl.Name, len(decl.Body), maxMacroBodyBytes)
	}

	graph := make(map[string][]string, len(existing)+1)
	for _, d := range existing {
		graph[d.Name] = extractMacroRefs(d.Body)
	}
	graph[decl.Name] = extractMacroRefs(decl.Body)

	if chain := detectCycle(graph, decl.Name); chain != nil {
		return compiler.SourceError(pos,
			"@macro %s: recursion cycle %s",
			decl.Name, strings.Join(chain, " -> "))
	}
	return nil
}

// extractMacroRefs scans a macro body for `@name` references, skipping
// string literals and line comments. Names are deduplicated in
// first-seen order so a body that calls the same macro twice counts as
// one edge.
func extractMacroRefs(body string) []string {
	var refs []string
	seen := make(map[string]bool)

	for i := 0; i < len(body); {
		switch body[i] {
		case '"', '\'', '`':
			i = skipQuoted(body, i)
		case '#':
			i = skipLineComment(body, i)
		case '/':
			i = skipSlashOrComment(body, i)
		case '@':
			var name string
			name, i = readMacroRefAt(body, i)
			if name != "" && !seen[name] {
				seen[name] = true
				refs = append(refs, name)
			}
		default:
			i++
		}
	}
	return refs
}

// skipQuoted advances past a quoted string starting at s[i] (which must
// be a quote byte). Escapes inside are honored; unterminated strings
// consume the rest of the input.
func skipQuoted(s string, i int) int {
	quote := s[i]
	i++
	for i < len(s) {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			i += 2
		case s[i] == quote:
			return i + 1
		default:
			i++
		}
	}
	return i
}

// skipLineComment advances past the remainder of the current line,
// stopping just before the newline so the outer loop can observe it as
// ordinary text.
func skipLineComment(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		i++
	}
	return i
}

// skipSlashOrComment advances past `//...` when s[i:i+2] is a line
// comment, or past the single slash otherwise.
func skipSlashOrComment(s string, i int) int {
	if i+1 < len(s) && s[i+1] == '/' {
		return skipLineComment(s, i)
	}
	return i + 1
}

// readMacroRefAt reads `@name` starting at s[i] (which must be '@').
// Returns the name and the index past it. Returns ("", i+1) when the
// character after '@' is not a valid identifier start.
func readMacroRefAt(s string, i int) (ref string, next int) {
	if i+1 >= len(s) || !isMacroNameStart(s[i+1]) {
		return "", i + 1
	}
	j := i + 1
	for j < len(s) && (isMacroNameCont(s[j]) || s[j] == '-' || s[j] == '.') {
		j++
	}
	return s[i+1 : j], j
}

func isMacroNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isMacroNameCont(c byte) bool {
	return isMacroNameStart(c) || (c >= '0' && c <= '9')
}

// detectCycle walks forward from start. If we ever reach start again,
// the new declaration would close a cycle. The chain is returned for
// the error message; nil means no cycle.
func detectCycle(graph map[string][]string, start string) []string {
	path := []string{start}
	visiting := map[string]bool{start: true}

	var walk func(node string) []string
	walk = func(node string) []string {
		for _, next := range graph[node] {
			if next == start {
				return append(append([]string{}, path...), start)
			}
			if visiting[next] {
				continue
			}
			visiting[next] = true
			path = append(path, next)
			if chain := walk(next); chain != nil {
				return chain
			}
			path = path[:len(path)-1]
			delete(visiting, next)
		}
		return nil
	}
	return walk(start)
}

func parseParams(raw string, pos compiler.Position, macroName string) (params, kinds []string, err error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil, nil
	}
	parts := strings.Split(raw, ",")
	params = make([]string, 0, len(parts))
	kinds = make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, nil, compiler.SourceError(pos, "@macro %s: empty parameter", macroName)
		}
		name, kind, typed := strings.Cut(part, ":")
		name = strings.TrimSpace(name)
		if !validIdentifier(name) {
			return nil, nil, compiler.SourceError(pos, "@macro %s: invalid parameter name %q", macroName, name)
		}
		if _, exists := seen[name]; exists {
			return nil, nil, compiler.SourceError(pos, "@macro %s: duplicate parameter %q", macroName, name)
		}
		seen[name] = struct{}{}
		parameterKind := ""
		if typed {
			parameterKind = strings.TrimSpace(kind)
			switch parameterKind {
			case "Identifier", "String", "Integer", "Expr", "ActionRef":
			default:
				return nil, nil, compiler.SourceError(pos,
					"@macro %s: parameter %s has unsupported kind %q (want Identifier, String, Integer, Expr, or ActionRef)",
					macroName, name, parameterKind)
			}
		}
		params = append(params, name)
		kinds = append(kinds, parameterKind)
	}
	return params, kinds, nil
}

func validIdentifier(name string) bool {
	if name == "" || !isMacroNameStart(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isMacroNameCont(name[i]) {
			return false
		}
	}
	return true
}

func validMacroName(name string) bool {
	if name == "" || !isMacroNameStart(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isMacroNameCont(name[i]) && name[i] != '-' {
			return false
		}
	}
	return true
}

// readBody reads the body of a `@macro ... { ... }` declaration.
//
// Inline form on one line:  `@macro x { runtime.const @{ value: "hi" } }`
// Multi-line form:          opening brace ends the header line, body
//
//	spans until a line whose only content is `}`.
//
// Returns the trimmed body, the 1-based file line where the body
// content starts, and the 1-based line after the terminator. The
// body-line value is used to remap parse errors from inside the body
// to the correct file position:
//
//   - inline body (content after '{' on the same line): bodyLine == start+1
//   - multi-line body (content begins on the next line): bodyLine == start+2
//
// start is 0-based, so start+1 is the 1-based file line of the header.
func readBody(lines []string, start, openBrace int) (body string, bodyLine, next int, err error) {
	line := lines[start]
	after := line[openBrace+1:]

	// Inline: closing brace on the same line, body is what's between
	// the braces.
	if content, trailing, found := strings.CutLast(after, "}"); found {
		if strings.TrimSpace(trailing) == "" {
			return strings.TrimSpace(content), start + 1, start + 1, nil
		}
	}

	var parts []string
	inline := strings.TrimSpace(after) != ""
	if inline {
		parts = append(parts, after)
	}

	depth := 1
	for j := start + 1; j < len(lines); j++ {
		currentLine := lines[j]
		delta := lineBraceDelta(currentLine)
		if depth+delta <= 0 && strings.TrimSpace(currentLine) == "}" {
			bodyLine := start + 2
			if inline {
				bodyLine = start + 1
			}
			return strings.TrimSpace(strings.Join(parts, "\n")), bodyLine, j + 1, nil
		}
		depth += delta
		parts = append(parts, currentLine)
	}
	return "", 0, 0, errors.New("unclosed '{'")
}

func lineBraceDelta(line string) int {
	delta := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '#' || (c == '/' && i+1 < len(line) && line[i+1] == '/') {
			break
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}
