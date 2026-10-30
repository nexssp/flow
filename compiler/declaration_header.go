package compiler

import "strings"

// ParseDeclarationHeader reads a named directive's inline modifiers and its
// leading modifier-only continuation lines. Blank lines and whole-line comments
// may separate continuations. The first other significant line starts the body
// permanently; its text and all subsequent lines are left untouched. A
// continuation must be indented further than the directive itself.
//
// Values are single-line unquoted scalars or single/double/backtick-quoted
// strings. Colons delimit modifiers outside quotes; # and // introduce comments.
// Next is the first line after the last consumed header line, so trivia before
// the body retains its original placement. No source lines are joined or edited.
func ParseDeclarationHeader(req DirectiveReq, directive string) (name string, mods []string, next int, err error) {
	line := req.Lines[req.I]
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "@"+directive))
	end := strings.IndexAny(text, ": \t#")
	if end < 0 {
		end = len(text)
	}
	name = text[:end]
	pos := Position{File: req.File, Line: req.I + 1}
	if name == "" || strings.HasPrefix(name, "//") {
		return "", nil, req.I + 1, SourceError(pos, "@%s requires a name", directive)
	}
	mods, err = parseDeclarationModifiers(text[end:], pos)
	if err != nil {
		return "", nil, req.I + 1, err
	}
	next = req.I + 1
	indent := headerIndent(line)
	for i := next; i < len(req.Lines); i++ {
		text = strings.TrimSpace(req.Lines[i])
		if text == "" || headerComment(text) {
			continue
		}
		if !strings.HasPrefix(text, ":") || headerIndent(req.Lines[i]) <= indent {
			break
		}
		extra, parseErr := parseDeclarationModifiers(text, Position{File: req.File, Line: i + 1})
		if parseErr != nil {
			return "", nil, next, parseErr
		}
		mods = append(mods, extra...)
		next = i + 1
	}
	return name, mods, next, nil
}

// ParseDeclarationHeaderWithSpans is ParseDeclarationHeader plus source spans
// for the directive keyword, declared name, and parsed header. The existing
// parser remains the single authority for header grammar and continuation rules.

type DeclarationHeader struct {
	Name        string
	Modifiers   []string
	Next        int
	KeywordSpan SourceSpan
	NameSpan    SourceSpan
	HeaderSpan  SourceSpan
}

func ParseDeclarationHeaderWithSpans(req DirectiveReq, directive string) (DeclarationHeader, error) {
	name, mods, next, err := ParseDeclarationHeader(req, directive)
	if err != nil {
		return DeclarationHeader{Next: next}, err
	}
	header := DeclarationHeader{Name: name, Modifiers: mods, Next: next}
	line := req.Lines[req.I]
	keyword := "@" + directive
	keywordStart := strings.Index(line, keyword)
	if keywordStart < 0 {
		return header, nil
	}
	header.KeywordSpan = SourceSpan{Start: req.Offset(req.I, keywordStart), End: req.Offset(req.I, keywordStart+len(keyword))}
	nameStart := keywordStart + len(keyword)
	for nameStart < len(line) && (line[nameStart] == ' ' || line[nameStart] == '\t') {
		nameStart++
	}
	if strings.HasPrefix(line[nameStart:], name) {
		header.NameSpan = SourceSpan{Start: req.Offset(req.I, nameStart), End: req.Offset(req.I, nameStart+len(name))}
	}
	headerEndLine := min(max(next-1, req.I), len(req.Lines)-1)
	header.HeaderSpan = SourceSpan{Start: header.KeywordSpan.Start, End: req.Offset(headerEndLine, len(req.Lines[headerEndLine]))}
	return header, nil
}

func headerIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func headerComment(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//")
}

func parseDeclarationModifiers(text string, pos Position) ([]string, error) {
	var mods []string
	for {
		text = strings.TrimSpace(text)
		if text == "" || headerComment(text) {
			return mods, nil
		}
		if text[0] != ':' {
			return nil, SourceError(pos, "expected ':' before declaration modifier, got %q", text)
		}
		text = text[1:]
		i := 0
		for i < len(text) && headerKeyByte(text[i], i == 0) {
			i++
		}
		if i == 0 {
			return nil, SourceError(pos, "expected declaration modifier name")
		}
		mod := text[:i]
		text = text[i:]
		if strings.HasPrefix(text, "=") {
			text = text[1:]
			value, rest, err := declarationValue(text, pos)
			if err != nil {
				return nil, err
			}
			mod += "=" + value
			text = rest
		}
		mods = append(mods, mod)
	}
}

func headerKeyByte(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		!first && (c >= '0' && c <= '9' || c == '-' || c == '.')
}

func declarationValue(text string, pos Position) (value, rest string, err error) {
	if text == "" || text[0] == ':' || text[0] == ' ' || text[0] == '\t' || headerComment(text) {
		return "", "", SourceError(pos, "expected declaration modifier value")
	}
	if quote := text[0]; quote == '\'' || quote == '"' || quote == '`' {
		var out strings.Builder
		for i := 1; i < len(text); i++ {
			c := text[i]
			if c == '\\' && i+1 < len(text) {
				i++
				out.WriteByte(text[i])
				continue
			}
			if c == quote {
				return out.String(), text[i+1:], nil
			}
			out.WriteByte(c)
		}
		return "", "", SourceError(pos, "unterminated quoted declaration modifier value")
	}
	i := 0
	for i < len(text) && !strings.ContainsRune(": \t\r", rune(text[i])) && !headerComment(text[i:]) {
		i++
	}
	return text[:i], text[i:], nil
}
