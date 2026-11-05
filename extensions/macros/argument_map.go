package macros

import (
	"fmt"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// ExpandArgumentMap implements compiler.ArgumentMapExpander. Map contents are
// still parsed by compiler.parseAtBrace; this extension only replaces a whole
// entry of the form @name(args) with the macro body before that parse begins.
func (m *macroPrimary) ExpandArgumentMap(p *compiler.Parser, raw string) (string, error) {
	return m.expandMapContent(p, raw)
}

func (m *macroPrimary) expandMapContent(p *compiler.Parser, raw string) (string, error) {
	entries := splitArgumentEntries(raw)
	for i, entry := range entries {
		name, args, ok, err := readMapMacroCall(entry)
		if err != nil {
			return "", err
		}
		if ok {
			declaration, found := m.declaration(name)
			if !found {
				// @config.* and @flag.* are reserved reference families, not
				// macros. Leave them to the ordinary map parser for diagnosis.
				if _, reserved := reservedDirectivePrefix(name); !reserved {
					return "", fmt.Errorf("unknown macro @%s in argument map", name)
				}
			} else {
				expanded, expandErr := m.expandMapMacro(p, name, args, declaration)
				if expandErr != nil {
					return "", expandErr
				}
				entries[i] = expanded
				continue
			}
		}

		entries[i], err = m.expandNestedMapBodies(p, entry)
		if err != nil {
			return "", err
		}
	}
	return strings.Join(entries, ","), nil
}

func (m *macroPrimary) declaration(name string) (Declaration, bool) {
	if declaration, ok := m.byName[name]; ok {
		return declaration, true
	}
	for _, builtin := range DefaultBuiltinMacros {
		if builtin.Name == name {
			return builtin, true
		}
	}
	return Declaration{}, false
}

func (m *macroPrimary) expandMapMacro(
	p *compiler.Parser,
	name string,
	args []string,
	declaration Declaration,
) (string, error) {
	invokeLine := p.Position().Line
	if len(args) != len(declaration.Params) {
		return "", compiler.SourceError(
			compiler.Position{File: p.File(), Line: invokeLine},
			"macro @%s: received %d arguments, want exactly %d",
			name, len(args), len(declaration.Params))
	}
	if err := validateTypedArguments(p, name, invokeLine, declaration, args); err != nil {
		return "", err
	}
	if m.depth >= maxMacroDepth {
		return "", compiler.SourceError(
			compiler.Position{File: p.File(), Line: invokeLine},
			"macro @%s: expansion depth exceeded (%d)", name, maxMacroDepth)
	}

	body := substituteParams(declaration.Body, declaration.Params, args)
	m.depth++
	expanded, err := m.expandMapContent(p, body)
	m.depth--

	recordExpansion(p, Expansion{
		Macro:      name,
		DefLine:    declaration.DefLine,
		InvokeLine: invokeLine,
		Where:      whereFromContext(p.Context()),
		Body:       body,
		Err:        err,
	})
	if err != nil {
		return "", wrapMacroError(err, p, name, invokeLine, declaration)
	}
	return expanded, nil
}

// readMapMacroCall accepts a macro invocation only when it occupies the full
// entry (aside from leading/trailing whitespace and line comments).
func readMapMacroCall(entry string) (name string, args []string, ok bool, err error) {
	i := skipMapTrivia(entry, 0)
	if i >= len(entry) {
		return "", nil, false, nil
	}
	if qualified, next, found := readQualifiedMacroRefAt(entry, i); found {
		name, i = qualified, next
	} else {
		if entry[i] != '@' {
			return "", nil, false, nil
		}
		name, i = readMacroRefAt(entry, i)
		if name == "" {
			return "", nil, false, nil
		}
	}
	i = skipMapSpace(entry, i)
	if i < len(entry) && entry[i] == '(' {
		end, inner, balanced := readCallBody(entry, i)
		if !balanced {
			return "", nil, false, fmt.Errorf("macros: unclosed '(' in argument-map macro call @%s", name)
		}
		if strings.TrimSpace(inner) != "" {
			for _, arg := range compiler.SplitTopLevel(inner, ',') {
				if arg = strings.TrimSpace(arg); arg != "" {
					args = append(args, arg)
				}
			}
		}
		i = end
	}
	if skipMapTrivia(entry, i) != len(entry) {
		return "", nil, false, nil
	}
	return name, args, true, nil
}

func readCallBody(s string, open int) (end int, body string, ok bool) {
	depth := 1
	var quote byte
	for i := open + 1; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if isMapCommentAt(s, i) {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, s[open+1 : i], true
			}
		}
	}
	return 0, "", false
}

func splitArgumentEntries(s string) []string {
	var entries []string
	var state argumentEntryScanState
	start := 0
	for i := 0; i < len(s); i++ {
		if state.quote == 0 && isMapCommentAt(s, i) {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		skipped, split := state.consume(s, i)
		i += skipped
		if split {
			entries = append(entries, s[start:i-skipped])
			start = i + 1
		}
	}
	return append(entries, s[start:])
}

type argumentEntryScanState struct {
	braces   int
	brackets int
	parens   int
	quote    byte
}

func (state *argumentEntryScanState) consume(s string, i int) (int, bool) {
	c := s[i]
	if state.quote != 0 {
		if c == '\\' && i+1 < len(s) {
			return 1, false
		}
		if c == state.quote {
			state.quote = 0
		}
		return 0, false
	}
	switch c {
	case '"', '\'', '`':
		state.quote = c
	case '{':
		state.braces++
	case '}':
		if state.braces > 0 {
			state.braces--
		}
	case '[':
		state.brackets++
	case ']':
		if state.brackets > 0 {
			state.brackets--
		}
	case '(':
		state.parens++
	case ')':
		if state.parens > 0 {
			state.parens--
		}
	case ',':
		return 0, state.atTopLevel()
	}
	return 0, false
}

func (state *argumentEntryScanState) atTopLevel() bool {
	return state.braces == 0 && state.brackets == 0 && state.parens == 0
}

func (m *macroPrimary) expandNestedMapBodies(p *compiler.Parser, s string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '"' || s[i] == '\'' || s[i] == '`' {
			end := skipQuoted(s, i)
			out.WriteString(s[i:end])
			i = end
			continue
		}
		if isMapCommentAt(s, i) {
			end := skipLineComment(s, i)
			out.WriteString(s[i:end])
			i = end
			continue
		}
		if s[i] != '{' {
			out.WriteByte(s[i])
			i++
			continue
		}
		end, ok := matchingMapBrace(s, i)
		if !ok {
			out.WriteByte(s[i])
			i++
			continue
		}
		inner, err := m.expandMapContent(p, s[i+1:end])
		if err != nil {
			return "", err
		}
		out.WriteByte('{')
		out.WriteString(inner)
		out.WriteByte('}')
		i = end + 1
	}
	return out.String(), nil
}

func matchingMapBrace(s string, open int) (int, bool) {
	depth := 1
	for i := open + 1; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\'' || c == '`' {
			i = skipQuoted(s, i) - 1
			continue
		}
		if isMapCommentAt(s, i) {
			i = skipLineComment(s, i)
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func skipMapTrivia(s string, i int) int {
	for i < len(s) {
		before := i
		i = skipMapSpace(s, i)
		if isMapCommentAt(s, i) {
			i = skipLineComment(s, i)
		}
		if before == i {
			break
		}
	}
	return i
}

func skipMapSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	return i
}

func isMapCommentAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	if s[i] == '/' && i+1 < len(s) && s[i+1] == '/' {
		return true
	}
	if s[i] != '#' {
		return false
	}
	if i+1 < len(s) {
		next := s[i+1]
		if next == ' ' || next == '\t' || next == '\r' || next == '\n' {
			return true
		}
	}
	for j := i - 1; j >= 0 && s[j] != '\n'; j-- {
		if s[j] != ' ' && s[j] != '\t' && s[j] != '\r' {
			return false
		}
	}
	return true
}
