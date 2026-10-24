// Package formatter provides conservative, deterministic formatting for NFlow source.
// It preserves expression text, comments, strings, and opaque directive bodies.
package formatter

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/nexssp/flow/compiler"
	requireext "github.com/nexssp/flow/extensions/require"
)

// Formatter formats source using the parser extensions supplied by the caller.
type Formatter struct {
	operators      *compiler.OperatorTable
	primaries      *compiler.PrimaryExtensionTable
	requireEnabled bool
}

// New builds a formatter from the active Flow bundles. The same bundles should
// be supplied by the host that exposes the formatter, so extension syntax is
// parsed consistently with that host.
func New(bundles []compiler.Bundle) *Formatter {
	var operators []compiler.Operator
	var primaries []compiler.PrimaryExtension
	requireEnabled := false
	for index := range bundles {
		bundle := &bundles[index]
		operators = append(operators, bundle.Operators...)
		primaries = append(primaries, bundle.Primaries...)
		for _, directive := range bundle.Directives {
			if directive.Name == "require" {
				requireEnabled = true
			}
		}
	}
	return &Formatter{
		operators:      compiler.NewOperatorTable(operators...),
		primaries:      compiler.NewPrimaryExtensionTable(primaries...),
		requireEnabled: requireEnabled,
	}
}

// Format returns canonical source or a source-positioned parse error. It does
// not preprocess the file, load includes, resolve actions, or execute code.
func (f *Formatter) Format(ctx context.Context, source, file string) (string, error) {
	if f == nil || f.operators == nil || f.primaries == nil {
		return "", errors.New("formatter: parser tables are unavailable")
	}
	source = strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	if source == "" {
		return "", nil
	}
	lines := strings.Split(source, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if err := f.validate(ctx, lines, file); err != nil {
		return "", err
	}
	out, err := f.formatLines(lines, file, opaqueSourceLines(source, lines))
	if err != nil {
		return "", err
	}
	return canonicalSource(out), nil
}

type formatState struct {
	rawDepth int
}

func (f *Formatter) formatLines(lines []string, file string, opaqueLines map[int]bool) ([]string, error) {
	state := formatState{}
	out := make([]string, 0, len(lines)+8)
	for i := 0; i < len(lines); {
		formatted, next, err := f.formatLine(lines, i, file, opaqueLines, &state)
		if err != nil {
			return nil, err
		}
		out = append(out, formatted...)
		i = next
	}
	return out, nil
}

func (f *Formatter) formatLine(lines []string, index int, file string, opaqueLines map[int]bool, state *formatState) (formatted []string, next int, err error) {
	line := lines[index]
	trimmed := strings.TrimSpace(line)
	indent := leadingWhitespace(line)
	if state.rawDepth > 0 {
		state.rawDepth += braceDelta(line)
		if state.rawDepth <= 0 {
			state.rawDepth = 0
		}
		return []string{line}, index + 1, nil
	}
	if opaqueLines[index] {
		return []string{line}, index + 1, nil
	}
	if trimmed == "" {
		return []string{""}, index + 1, nil
	}
	if directive, ok := declarationDirective(trimmed); ok {
		return formatDeclaration(lines, index, file, directive)
	}
	if trimmed == "@end" || trimmed == "}" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return []string{indent + trimmed}, index + 1, nil
	}
	if isDirectiveLine(trimmed) || containsMacroInvocation(trimmed) {
		return f.formatDirectiveLine(lines, index, file, trimmed, state)
	}
	return formatExpressionLines(line), index + 1, nil
}

func formatDeclaration(lines []string, index int, file, directive string) (formatted []string, next int, err error) {
	name, _, next, err := compiler.ParseDeclarationHeader(compiler.DirectiveReq{
		Lines: lines, I: index, File: file,
	}, directive)
	if err != nil {
		return nil, index + 1, err
	}
	if next <= index {
		next = index + 1
	}
	indent := leadingWhitespace(lines[index])
	base, firstMods, firstComment := splitDeclarationLine(lines[index], directive, name)
	out := []string{base}
	appendHeaderParts(&out, indent, firstMods, firstComment)
	for i := index + 1; i < next; i++ {
		part := strings.TrimSpace(lines[i])
		if part == "" {
			out = append(out, "")
			continue
		}
		if isHeaderComment(part) {
			out = append(out, indent+"  "+part)
			continue
		}
		mods, comment := splitHeaderModifiers(part)
		appendHeaderParts(&out, indent, mods, comment)
	}
	return out, next, nil
}

func (f *Formatter) formatDirectiveLine(lines []string, index int, file, trimmed string, state *formatState) (formatted []string, next int, err error) {
	line := lines[index]
	if f.requireEnabled && directiveName(trimmed) == "require" {
		nextLine, parseErr := f.parseRequireOptions(lines, index, file)
		if parseErr != nil {
			return nil, index + 1, parseErr
		}
		return lines[index:nextLine], nextLine, nil
	}
	if isRawBlockDirective(trimmed) {
		state.rawDepth = max(braceDelta(line), 0)
	}
	return []string{line}, index + 1, nil
}

func canonicalSource(lines []string) string {
	canonical := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" && (len(canonical) == 0 || canonical[len(canonical)-1] == "") {
			continue
		}
		canonical = append(canonical, line)
	}
	for len(canonical) > 0 && canonical[0] == "" {
		canonical = canonical[1:]
	}
	for len(canonical) > 0 && canonical[len(canonical)-1] == "" {
		canonical = canonical[:len(canonical)-1]
	}
	if len(canonical) == 0 {
		return ""
	}
	return strings.Join(canonical, "\n") + "\n"
}

func opaqueSourceLines(source string, lines []string) map[int]bool {
	opaque := make(map[int]bool)
	lexer := compiler.NewLexer(source)
	for {
		token := lexer.Next()
		if token.Type == compiler.TokEOF {
			break
		}
		if token.Type != compiler.TokString && token.Type != compiler.TokAtBrace {
			continue
		}
		span := strings.Count(token.Lit, "\n")
		if span == 0 {
			continue
		}
		first := token.Line - 1
		last := min(len(lines)-1, first+span)
		for line := first; line <= last; line++ {
			if line >= 0 {
				opaque[line] = true
			}
		}
	}
	return opaque
}

func (f *Formatter) validate(ctx context.Context, lines []string, file string) error {
	state := validationState{}
	for index := 0; index < len(lines); {
		next, err := f.validateLine(ctx, lines, index, file, &state)
		if err != nil {
			return err
		}
		if next <= index {
			next = index + 1
		}
		index = next
	}
	if err := state.flush(ctx, f.operators, f.primaries, file); err != nil {
		return err
	}
	switch {
	case state.pipeline:
		return compiler.SourceError(compiler.Position{File: file, Line: len(lines)}, "@pipeline is missing @end")
	case state.rawDepth > 0:
		return compiler.SourceError(compiler.Position{File: file, Line: len(lines)}, "@%s block is not closed", state.rawDirective)
	case state.scopeDepth > 0:
		return compiler.SourceError(compiler.Position{File: file, Line: len(lines)}, "@scope block is not closed")
	default:
		return nil
	}
}

type validationState struct {
	segment      []string
	segmentLine  int
	pipeline     bool
	scopeDepth   int
	rawDepth     int
	rawDirective string
}

func (state *validationState) flush(ctx context.Context, operators *compiler.OperatorTable, primaries *compiler.PrimaryExtensionTable, file string) error {
	if len(state.segment) == 0 {
		return nil
	}
	text := strings.Join(state.segment, "\n")
	state.segment = nil
	if strings.TrimSpace(text) == "" || compiler.NewLexer(text).Next().Type == compiler.TokEOF || containsMacroInvocation(text) {
		return nil
	}
	_, err := compiler.NewParserWithFileOffset(ctx, operators, primaries, text, file, state.segmentLine-1).Parse()
	return err
}

func (state *validationState) addLine(index int, line string) {
	if len(state.segment) == 0 {
		state.segmentLine = index + 1
	}
	state.segment = append(state.segment, line)
}

func (f *Formatter) validateLine(ctx context.Context, lines []string, index int, file string, state *validationState) (int, error) {
	line := lines[index]
	trimmed := strings.TrimSpace(line)
	if state.rawDepth > 0 {
		state.rawDepth += braceDelta(line)
		if state.rawDepth <= 0 {
			state.rawDepth = 0
			state.rawDirective = ""
		}
		return index + 1, nil
	}
	if trimmed == "" {
		return index + 1, state.flush(ctx, f.operators, f.primaries, file)
	}
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		state.addLine(index, line)
		return index + 1, nil
	}
	if trimmed == "@end" {
		if err := state.flush(ctx, f.operators, f.primaries, file); err != nil {
			return index + 1, err
		}
		state.pipeline = false
		return index + 1, nil
	}
	if strings.HasPrefix(trimmed, "@pipeline") {
		return f.validateDeclaration(ctx, lines, index, file, "pipeline", state)
	}
	if strings.HasPrefix(trimmed, "@profile") {
		return f.validateDeclaration(ctx, lines, index, file, "profile", state)
	}
	if strings.HasPrefix(trimmed, "@scope") {
		if err := state.flush(ctx, f.operators, f.primaries, file); err != nil {
			return index + 1, err
		}
		if strings.HasSuffix(trimmed, "{") {
			state.scopeDepth++
		}
		return index + 1, nil
	}
	if state.scopeDepth > 0 && trimmed == "}" {
		if err := state.flush(ctx, f.operators, f.primaries, file); err != nil {
			return index + 1, err
		}
		state.scopeDepth--
		return index + 1, nil
	}
	if isDirectiveLine(trimmed) {
		return f.validateDirective(ctx, lines, index, file, line, trimmed, state)
	}
	if !state.pipeline && state.scopeDepth == 0 && isMacroInvocation(trimmed) {
		return index + 1, state.flush(ctx, f.operators, f.primaries, file)
	}
	state.addLine(index, line)
	return index + 1, nil
}

func (f *Formatter) validateDeclaration(ctx context.Context, lines []string, index int, file, directive string, state *validationState) (int, error) {
	if err := state.flush(ctx, f.operators, f.primaries, file); err != nil {
		return index + 1, err
	}
	_, _, next, err := compiler.ParseDeclarationHeader(compiler.DirectiveReq{Lines: lines, I: index, File: file}, directive)
	if err != nil {
		return index + 1, err
	}
	if directive == "pipeline" {
		if err := rejectDeprecatedPipelineSchema(lines, index, next, file); err != nil {
			return index + 1, err
		}
		state.pipeline = true
	}
	return next, nil
}

func (f *Formatter) validateDirective(ctx context.Context, lines []string, index int, file, line, trimmed string, state *validationState) (int, error) {
	if err := state.flush(ctx, f.operators, f.primaries, file); err != nil {
		return index + 1, err
	}
	if f.requireEnabled && directiveName(trimmed) == "require" {
		_, next, err := requireext.ParseDirectiveOptions(lines, index, file)
		return next, err
	}
	assertion, hasAssertion := strings.CutPrefix(trimmed, "@assert:")
	if hasAssertion {
		if err := validateAssertion(strings.TrimSpace(assertion), file, index); err != nil {
			return index + 1, err
		}
	}
	if isRawBlockDirective(trimmed) {
		state.rawDepth = braceDelta(line)
		if state.rawDepth > 0 {
			state.rawDirective = directiveName(trimmed)
		}
	}
	return index + 1, nil
}

func validateAssertion(assertion, file string, index int) error {
	if assertion == "" || strings.HasPrefix(assertion, "{") {
		return nil
	}
	if _, err := compiler.CompileExpression(assertion); err != nil {
		return compiler.SourceError(compiler.Position{File: file, Line: index + 1}, "invalid @assert expression: %v", err)
	}
	return nil
}

func (f *Formatter) parseRequireOptions(lines []string, start int, file string) (int, error) {
	_, next, err := requireext.ParseDirectiveOptions(lines, start, file)
	return next, err
}

func rejectDeprecatedPipelineSchema(lines []string, start, end int, file string) error {
	for lineIndex := start; lineIndex < end && lineIndex < len(lines); lineIndex++ {
		if column := deprecatedSchemaModifierColumn(lines[lineIndex]); column > 0 {
			return compiler.SourceError(compiler.Position{
				File: file,
				Line: lineIndex + 1,
				Col:  column,
			}, ":schema is no longer supported; use :request-schema=Name")
		}
	}
	return nil
}

// deprecatedSchemaModifierColumn returns the one-based byte column of a real
// :schema modifier in a declaration header. It ignores quoted values and
// trailing comments, matching the declaration-header grammar.
func deprecatedSchemaModifierColumn(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		var handled bool
		quote, i, handled = advanceHeaderQuote(line, i, quote)
		if handled {
			continue
		}
		switch line[i] {
		case '#':
			return 0
		case '/':
			if hasHeaderLineComment(line, i) {
				return 0
			}
		case ':':
			if isDeprecatedSchemaModifier(line, i) {
				return i + 1
			}
		}
	}
	return 0
}

func advanceHeaderQuote(line string, index int, quote byte) (nextQuote byte, nextIndex int, handled bool) {
	char := line[index]
	if quote != 0 {
		if char == '\\' && index+1 < len(line) {
			return quote, index + 1, true
		}
		if char == quote {
			return 0, index, true
		}
		return quote, index, true
	}
	if char == '\'' || char == '"' || char == '`' {
		return char, index, true
	}
	return 0, index, false
}

func hasHeaderLineComment(line string, index int) bool {
	return index+1 < len(line) && line[index+1] == '/'
}

func isDeprecatedSchemaModifier(line string, index int) bool {
	keyStart := index + 1
	keyEnd := keyStart
	for keyEnd < len(line) && declarationHeaderKeyByte(line[keyEnd], keyEnd == keyStart) {
		keyEnd++
	}
	if line[keyStart:keyEnd] != "schema" {
		return false
	}
	if keyEnd == len(line) {
		return true
	}
	switch line[keyEnd] {
	case '=', ':', ' ', '\t', '\r', '\n', '#', '/':
		return true
	default:
		return false
	}
}

func declarationHeaderKeyByte(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		!first && (c >= '0' && c <= '9' || c == '-' || c == '.')
}

func declarationDirective(line string) (string, bool) {
	for _, name := range []string{"pipeline", "profile"} {
		if line == "@"+name || strings.HasPrefix(line, "@"+name+" ") || strings.HasPrefix(line, "@"+name+"\t") || strings.HasPrefix(line, "@"+name+":") || strings.HasPrefix(line, "@"+name+"#") || strings.HasPrefix(line, "@"+name+"//") {
			return name, true
		}
	}
	return "", false
}

func splitDeclarationLine(line, directive, name string) (base string, mods []string, comment string) {
	indent := leadingWhitespace(line)
	trimmed := strings.TrimSpace(line)
	prefix := "@" + directive
	tail := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	tail = strings.TrimPrefix(tail, name)
	mods, comment = splitHeaderModifiers(tail)
	base = indent + prefix + " " + name
	if len(mods) == 0 && comment != "" {
		base += " " + comment
		comment = ""
	}
	return base, mods, comment
}

func appendHeaderParts(out *[]string, indent string, mods []string, comment string) {
	for index, mod := range mods {
		line := indent + "  " + mod
		if index == len(mods)-1 && comment != "" {
			line += " " + comment
		}
		*out = append(*out, line)
	}
	if len(mods) == 0 && comment != "" {
		*out = append(*out, indent+"  "+comment)
	}
}

// splitHeaderModifiers returns the ordered colon-prefixed modifiers and an
// optional trailing line comment. The caller separately validates syntax with
// compiler.ParseDeclarationHeader.
func splitHeaderModifiers(text string) (modifiers []string, comment string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ""
	}
	if isHeaderComment(text) {
		return nil, text
	}
	var parts []string
	quote := byte(0)
	start := -1
	for i := 0; i < len(text); i++ {
		c := text[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(text) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if headerCommentAt(text, i) {
			if start >= 0 {
				if part := strings.TrimSpace(text[start:i]); part != "" {
					parts = append(parts, part)
				}
			}
			return parts, strings.TrimSpace(text[i:])
		}
		if c == ':' {
			if start >= 0 {
				if part := strings.TrimSpace(text[start:i]); part != "" {
					parts = append(parts, part)
				}
			}
			start = i
		}
	}
	if start >= 0 {
		if part := strings.TrimSpace(text[start:]); part != "" {
			parts = append(parts, part)
		}
	}
	return parts, ""
}

func headerCommentAt(text string, i int) bool {
	return text[i] == '#' || i+1 < len(text) && text[i] == '/' && text[i+1] == '/'
}

func isHeaderComment(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//")
}

func formatExpressionLines(line string) []string {
	indent := leadingWhitespace(line)
	body := strings.TrimSpace(line)
	if body == "" {
		return []string{""}
	}
	lexer := compiler.NewLexer(body)
	var tokens []compiler.Token
	for {
		token := lexer.Next()
		tokens = append(tokens, token)
		if token.Type == compiler.TokEOF {
			break
		}
	}
	var arrows []int
	depth := 0
	for _, token := range tokens {
		switch token.Type {
		case compiler.TokLParen, compiler.TokLBrace:
			depth++
		case compiler.TokRParen, compiler.TokRBrace:
			if depth > 0 {
				depth--
			}
		case compiler.TokArrow:
			if depth == 0 {
				arrows = append(arrows, token.Offset)
			}
		case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
			compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr, compiler.TokColon,
			compiler.TokEquals, compiler.TokComma, compiler.TokQuestion, compiler.TokHash,
			compiler.TokTilde, compiler.TokAtBrace, compiler.TokAtPrompt:
			// These tokens do not affect delimiter nesting or arrow positions.
		}
	}
	if len(arrows) > 0 && strings.TrimSpace(body[:arrows[0]]) != "" && !hasSourceComment(body) {
		formatted := make([]string, 0, len(arrows)+1)
		first := formatModifierSpacing(strings.TrimSpace(body[:arrows[0]]))
		formatted = append(formatted, indent+first)
		for i, arrow := range arrows {
			end := len(body)
			if i+1 < len(arrows) {
				end = arrows[i+1]
			}
			part := strings.TrimSpace(body[arrow+2 : end])
			part = formatModifierSpacing(part)
			formatted = append(formatted, indent+"  -> "+part)
		}
		return formatted
	}
	formatted := formatModifierSpacing(body)
	return []string{indent + formatted}
}

func formatModifierSpacing(body string) string {
	lexer := compiler.NewLexer(body)
	var tokens []compiler.Token
	for {
		token := lexer.Next()
		tokens = append(tokens, token)
		if token.Type == compiler.TokEOF {
			break
		}
	}
	var changes []int
	depth := 0
	for i := 0; i+1 < len(tokens); i++ {
		current, next := tokens[i], tokens[i+1]
		if current.Type == compiler.TokColon && next.Offset == current.Offset+1 {
			if depth > 0 {
				changes = append(changes, current.Offset+1)
			} else if current.Offset > 0 && !isSpace(body[current.Offset-1]) {
				changes = append(changes, current.Offset)
			}
		}
		switch current.Type {
		case compiler.TokLParen, compiler.TokLBrace:
			depth++
		case compiler.TokRParen, compiler.TokRBrace:
			if depth > 0 {
				depth--
			}
		case compiler.TokEOF, compiler.TokIdent, compiler.TokString, compiler.TokNumber,
			compiler.TokArrow, compiler.TokPipe, compiler.TokAmpersand, compiler.TokOrOr, compiler.TokColon,
			compiler.TokEquals, compiler.TokComma, compiler.TokQuestion, compiler.TokHash,
			compiler.TokTilde, compiler.TokAtBrace, compiler.TokAtPrompt:
			// These tokens do not affect nesting or modifier spacing.
		}
	}
	for i := range slices.Backward(changes) {
		at := changes[i]
		body = body[:at] + " " + body[at:]
	}
	return body
}

func hasSourceComment(text string) bool {
	var quote byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(text) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if c == '/' && i+1 < len(text) && text[i+1] == '/' && (i == 0 || isSpace(text[i-1])) {
			return true
		}
		if c == '#' && (i == 0 || isSpace(text[i-1])) && (i+1 == len(text) || isSpace(text[i+1])) {
			return true
		}
	}
	return false
}

func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

func isDirectiveLine(line string) bool {
	return strings.HasPrefix(line, "@") && !strings.HasPrefix(line, "@{") && !isMacroInvocation(line)
}

func isMacroInvocation(line string) bool {
	if !strings.HasPrefix(line, "@") || strings.HasPrefix(line, "@{") {
		return false
	}
	open := strings.IndexByte(line, '(')
	return open > 1 && !strings.ContainsAny(line[:open], " \t")
}

func containsMacroInvocation(source string) bool {
	lexer := compiler.NewLexer(source)
	previousAtPrompt := false
	for {
		token := lexer.Next()
		if token.Type == compiler.TokEOF {
			return false
		}
		if previousAtPrompt && token.Type == compiler.TokLParen {
			return true
		}
		previousAtPrompt = token.Type == compiler.TokAtPrompt
	}
}

func directiveName(line string) string {
	name := strings.TrimPrefix(line, "@")
	for i, r := range name {
		if r == ':' || r == ' ' || r == '\t' || r == '{' {
			return name[:i]
		}
	}
	return name
}

func isRawBlockDirective(line string) bool {
	name := directiveName(line)
	switch name {
	case "macro", "schema", "symbols", "approval", "pool", "assert", "config":
		return strings.Contains(line, "{")
	default:
		return false
	}
}

func braceDelta(line string) int {
	delta := 0
	parenDepth := 0
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
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if c == '#' && parenDepth == 0 || c == '/' && i+1 < len(line) && line[i+1] == '/' {
			break
		}
		switch c {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}
