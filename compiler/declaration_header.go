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
	header, err := parseDeclarationHeaderDetailed(req, directive)
	if err != nil {
		return "", nil, header.Next, err
	}
	return header.Name, header.Modifiers, header.Next, nil
}

// ParseDeclarationHeaderWithSpans is ParseDeclarationHeader plus source spans
// for the directive keyword, declared name, and parsed header. QuotedModifiers
// records whether each modifier value was written with quote delimiters; callers
// that resolve symbols can therefore leave quoted literal values untouched.
// The existing parser remains the single authority for header grammar and
// continuation rules.

type DeclarationHeader struct {
	Name            string
	Modifiers       []string
	QuotedModifiers []bool
	Next            int
	KeywordSpan     SourceSpan
	NameSpan        SourceSpan
	HeaderSpan      SourceSpan
}

func ParseDeclarationHeaderWithSpans(req DirectiveReq, directive string) (DeclarationHeader, error) {
	header, err := parseDeclarationHeaderDetailed(req, directive)
	if err != nil {
		return DeclarationHeader{Next: header.Next}, err
	}
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
	if strings.HasPrefix(line[nameStart:], header.Name) {
		header.NameSpan = SourceSpan{Start: req.Offset(req.I, nameStart), End: req.Offset(req.I, nameStart+len(header.Name))}
	}
	headerEndLine := min(max(header.Next-1, req.I), len(req.Lines)-1)
	header.HeaderSpan = SourceSpan{Start: header.KeywordSpan.Start, End: req.Offset(headerEndLine, len(req.Lines[headerEndLine]))}
	return header, nil
}

func parseDeclarationHeaderDetailed(req DirectiveReq, directive string) (DeclarationHeader, error) {
	line := req.Lines[req.I]
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "@"+directive))
	end := strings.IndexAny(text, ": \t#")
	if end < 0 {
		end = len(text)
	}
	name := text[:end]
	pos := Position{File: req.File, Line: req.I + 1}
	if name == "" || strings.HasPrefix(name, "//") {
		return DeclarationHeader{Next: req.I + 1}, SourceError(pos, "@%s requires a name", directive)
	}
	mods, quoted, err := parseDeclarationModifiersDetailed(text[end:], pos)
	if err != nil {
		return DeclarationHeader{Next: req.I + 1}, err
	}
	next := req.I + 1
	indent := headerIndent(line)
	for i := next; i < len(req.Lines); i++ {
		text = strings.TrimSpace(req.Lines[i])
		if text == "" || headerComment(text) {
			continue
		}
		if !strings.HasPrefix(text, ":") || headerIndent(req.Lines[i]) <= indent {
			break
		}
		extra, extraQuoted, parseErr := parseDeclarationModifiersDetailed(text, Position{File: req.File, Line: i + 1})
		if parseErr != nil {
			return DeclarationHeader{Next: next}, parseErr
		}
		mods = append(mods, extra...)
		quoted = append(quoted, extraQuoted...)
		next = i + 1
	}
	return DeclarationHeader{Name: name, Modifiers: mods, QuotedModifiers: quoted, Next: next}, nil
}

func headerIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func headerComment(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//")
}

func parseDeclarationModifiersDetailed(text string, pos Position) (mods []string, quoted []bool, err error) {
	for {
		text = strings.TrimSpace(text)
		if text == "" || headerComment(text) {
			return mods, quoted, nil
		}
		if text[0] != ':' {
			return nil, nil, SourceError(pos, "expected ':' before declaration modifier, got %q", text)
		}
		text = text[1:]
		i := 0
		for i < len(text) && headerKeyByte(text[i], i == 0) {
			i++
		}
		if i == 0 {
			return nil, nil, SourceError(pos, "expected declaration modifier name")
		}
		mod := text[:i]
		text = text[i:]
		isQuoted := false
		if strings.HasPrefix(text, "=") {
			text = text[1:]
			value, rest, valueQuoted, err := declarationValueDetailed(text, pos)
			if err != nil {
				return nil, nil, err
			}
			mod += "=" + value
			text = rest
			isQuoted = valueQuoted
		}
		mods = append(mods, mod)
		quoted = append(quoted, isQuoted)
	}
}

func headerKeyByte(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		!first && (c >= '0' && c <= '9' || c == '-' || c == '.')
}

func declarationValueDetailed(text string, pos Position) (value, rest string, quoted bool, err error) {
	if text == "" || text[0] == ':' || text[0] == ' ' || text[0] == '\t' || headerComment(text) {
		return "", "", false, SourceError(pos, "expected declaration modifier value")
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
				return out.String(), text[i+1:], true, nil
			}
			out.WriteByte(c)
		}
		return "", "", false, SourceError(pos, "unterminated quoted declaration modifier value")
	}
	i := 0
	for i < len(text) && !strings.ContainsRune(": \t\r", rune(text[i])) && !headerComment(text[i:]) {
		i++
	}
	return text[:i], text[i:], false, nil
}
