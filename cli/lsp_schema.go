package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	flowschema "github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/runner"
)

type lspSchemaOccurrence struct {
	name        string
	start       int
	end         int
	declaration bool
}

type lspSchemaSymbol struct {
	name  string
	start int
	end   int
}

type lspSchemaIndex struct {
	declarations map[string]lspSchemaSymbol
	occurrences  []lspSchemaOccurrence
}

type lspSchemaCacheEntry struct {
	version      int
	source       string
	sourceIndex  compiler.SourceIndex
	prepared     *compiler.PreparedSource
	config       runner.Config
	prepareError error
	index        lspSchemaIndex
	schemas      []flowschema.Schema
}

type lspSchemaWorkspaceSource struct {
	uri         string
	source      string
	sourceIndex compiler.SourceIndex
	index       lspSchemaIndex
	open        bool
}

type lspSchemaReference struct {
	modifier   string
	start      int
	end        int
	name       string
	value      string
	nameStart  int
	nameEnd    int
	nameExists bool
}

type lspSchemaIssue struct {
	diagnostic lspDiagnostic
	fixTitle   string
	fixStart   int
	fixEnd     int
	fixText    string
}

func indexLSPFlowSchemas(source string) lspSchemaIndex {
	return lspSchemaIndexFromSourceIndex(lspSourceIndexFromText(source))
}

func lspSchemaIndexFromSourceIndex(sourceIndex compiler.SourceIndex) lspSchemaIndex {
	index := lspSchemaIndex{declarations: make(map[string]lspSchemaSymbol)}
	for _, declaration := range sourceIndex.Declarations {
		if declaration.Kind != lspSourceKindSchema || declaration.NameSpan.End <= declaration.NameSpan.Start {
			continue
		}
		symbol := lspSchemaSymbol{name: declaration.Name, start: declaration.NameSpan.Start, end: declaration.NameSpan.End}
		if _, exists := index.declarations[declaration.Name]; !exists {
			index.declarations[declaration.Name] = symbol
		}
		index.occurrences = append(index.occurrences, lspSchemaOccurrence{
			name: declaration.Name, start: symbol.start, end: symbol.end, declaration: true,
		})
	}
	for _, reference := range sourceIndex.References {
		if reference.Kind != lspSourceKindSchemaEmbed && reference.Kind != lspSourceKindSchema {
			continue
		}
		if reference.Name == "" || reference.NameSpan.End <= reference.NameSpan.Start {
			continue
		}
		index.occurrences = append(index.occurrences, lspSchemaOccurrence{
			name: reference.Name, start: reference.NameSpan.Start, end: reference.NameSpan.End,
		})
	}
	sort.SliceStable(index.occurrences, func(i, j int) bool { return index.occurrences[i].start < index.occurrences[j].start })
	return index
}

func (s *flowLanguageServer) schemaCacheEntry(uri, source string) lspSchemaCacheEntry {
	version := s.documentVersion[uri]
	if entry, exists := s.schemaCache[uri]; exists && entry.version == version && entry.source == source {
		return entry
	}
	sourceIndex, metadata, prepared, config, prepareErr := s.sourceIndexAndMetadata(uri, source)
	entry := lspSchemaCacheEntry{
		version: version, source: source, sourceIndex: sourceIndex, prepared: prepared, config: config,
		prepareError: prepareErr,
		index:        lspSchemaIndexFromSourceIndex(sourceIndex),
		schemas:      flowschema.SchemasFromMap(metadata),
	}
	if s.schemaCache == nil {
		s.schemaCache = make(map[string]lspSchemaCacheEntry)
	}
	s.schemaCache[uri] = entry
	return entry
}

func (s *flowLanguageServer) schemaWorkspace(uri string) []lspSchemaWorkspaceSource {
	source, exists := s.documents[uri]
	if !exists {
		return nil
	}
	entry := s.schemaCacheEntry(uri, source)
	return []lspSchemaWorkspaceSource{{uri: uri, source: source, sourceIndex: entry.sourceIndex, index: entry.index, open: true}}
}

func lspSchemaPathKey(path string) string {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	return filepath.Clean(path)
}

func lspSchemaSourceKey(uri string) string {
	if path := lspFileURIPath(uri); path != "" {
		return "file:" + lspSchemaPathKey(path)
	}
	return "uri:" + uri
}

func lspSchemaWorkspaceSourceAt(sources []lspSchemaWorkspaceSource, uri string) (lspSchemaWorkspaceSource, bool) {
	for i := range sources {
		if sources[i].uri == uri {
			return sources[i], true
		}
	}
	return lspSchemaWorkspaceSource{}, false
}

func lspSchemaWorkspaceDeclaration(sources []lspSchemaWorkspaceSource, name string) (lspSchemaWorkspaceSource, lspSchemaSymbol, bool) {
	for i := range sources {
		if declaration, exists := sources[i].index.declarations[name]; exists {
			return sources[i], declaration, true
		}
	}
	return lspSchemaWorkspaceSource{}, lspSchemaSymbol{}, false
}

func (s *flowLanguageServer) schemaCompletions(uri, source string, position lspPosition) []map[string]any {
	if source == "" {
		return nil
	}
	offset, ok := lspPositionToByteOffset(source, position)
	if !ok {
		return nil
	}
	entry := s.schemaCacheEntry(uri, source)
	if valueStart, valueEnd, prefix, inModifier := lspSchemaModifierCompletionRange(source, entry.sourceIndex, offset); inModifier {
		workspace := s.schemaWorkspace(uri)
		names := make([]string, 0)
		for i := range workspace {
			for name := range workspace[i].index.declarations {
				if strings.HasPrefix(name, prefix) {
					names = append(names, name)
				}
			}
		}
		sort.Strings(names)
		items := make([]map[string]any, 0, len(names))
		previous := ""
		for _, name := range names {
			if name == previous {
				continue
			}
			previous = name
			items = append(items, lspSchemaCompletionItem(name, "NFlow schema", 7, name, source, valueStart, valueEnd))
		}
		return items
	}

	fieldStart, fieldEnd, prefix, inFieldAccess := lspSchemaFieldCompletionRange(source, entry.sourceIndex, offset)
	if !inFieldAccess {
		return nil
	}
	schemaName := lspPipelineRequestSchemaAt(entry.sourceIndex, offset)
	if schemaName == "" {
		return nil
	}
	workspace := s.schemaWorkspace(uri)
	fieldSchema, found := s.lspSchemaForCompletion(schemaName, workspace)
	if !found {
		return nil
	}
	items := make([]map[string]any, 0, len(fieldSchema.Fields))
	seen := make(map[string]struct{}, len(fieldSchema.Fields))
	for _, field := range fieldSchema.Fields {
		name := field.JSONName
		if name == "" {
			name = field.Name
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		detail := fmt.Sprintf("%s field · %s", fieldSchema.Name, field.Type)
		if field.Name != "" && field.Name != name {
			detail += fmt.Sprintf(" (Go field %s)", field.Name)
		}
		items = append(items, lspSchemaCompletionItem(name, detail, 5, name, source, fieldStart, fieldEnd))
	}
	return items
}

func lspSchemaModifierCompletionRange(source string, index compiler.SourceIndex, cursor int) (start, end int, prefix string, ok bool) {
	if cursor < 0 || cursor > len(source) {
		return 0, 0, "", false
	}
	tokens := index.Tokens
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Type != compiler.TokColon || tokens[i+1].Type != compiler.TokIdent || tokens[i+2].Type != compiler.TokEquals {
			continue
		}
		modifier := tokens[i+1].Text
		if !lspIsSchemaModifier(modifier) {
			continue
		}
		start, end, ok = lspSchemaModifierValueRange(source, tokens, i+2, cursor)
		if !ok {
			continue
		}
		prefix = source[start:cursor]
		return start, end, prefix, true
	}
	return 0, 0, "", false
}

func lspIsSchemaModifier(modifier string) bool {
	return modifier == "request-schema" || modifier == "response-schema" || modifier == lspSourceKindSchema
}

func lspSchemaModifierValueRange(source string, tokens []compiler.SourceToken, equalsIndex, cursor int) (start, end int, ok bool) {
	line := lspPositionForByteOffset(source, tokens[equalsIndex].Span.End).Line
	start = tokens[equalsIndex].Span.End
	end = start
	valueIndex := equalsIndex + 1
	if valueIndex < len(tokens) && tokens[valueIndex].Type == compiler.TokIdent && tokens[valueIndex].Text == "!" && tokens[valueIndex].Span.Start == start {
		start = tokens[valueIndex].Span.End
		end = start
		valueIndex++
	}
	if valueIndex < len(tokens) {
		value := tokens[valueIndex]
		if value.Type == compiler.TokIdent && lspPositionForByteOffset(source, value.Span.Start).Line == line {
			if value.Span.Start <= cursor && cursor <= value.Span.End {
				start, end = value.Span.Start, value.Span.End
			} else if cursor >= start && cursor <= value.Span.Start {
				end = cursor
			}
		}
	}
	return start, end, cursor >= start && cursor <= end
}

func lspSchemaFieldCompletionRange(source string, index compiler.SourceIndex, cursor int) (start, end int, prefix string, ok bool) {
	if cursor < 0 || cursor > len(source) {
		return 0, 0, "", false
	}
	for _, token := range index.Tokens {
		if token.Type != compiler.TokIdent || !strings.HasPrefix(token.Text, ".") {
			continue
		}
		start = token.Span.Start + 1
		end = token.Span.End
		if cursor < start || cursor > end {
			continue
		}
		for i := start; i < end; i++ {
			if !isLSPIdentifierByte(source[i]) {
				return 0, 0, "", false
			}
		}
		prefix = source[start:cursor]
		return start, end, prefix, true
	}
	return 0, 0, "", false
}

func lspSchemaCompletionItem(label, detail string, kind int, insertText, source string, start, end int) map[string]any {
	return map[string]any{
		"label":      label,
		"kind":       kind,
		"detail":     detail,
		"insertText": insertText,
		"textEdit": map[string]any{
			lspNewTextKey: insertText,
			lspRangeKey:   lspRangeForByteOffsets(source, start, end),
		},
	}
}

func lspPipelineRequestSchemaAt(index compiler.SourceIndex, offset int) string {
	for _, block := range index.Blocks {
		if block.Kind != lspSourceKindPipeline || offset < block.BodySpan.Start || offset > block.BodySpan.End {
			continue
		}
		for _, reference := range index.References {
			if reference.Kind == lspSourceKindSchema && reference.Modifier == "request-schema" && reference.HasName &&
				reference.ModifierSpan.Start >= block.HeaderSpan.Start && reference.ModifierSpan.End <= block.HeaderSpan.End {
				return reference.Name
			}
		}
	}
	return ""
}

func (s *flowLanguageServer) lspSchemaForCompletion(name string, workspace []lspSchemaWorkspaceSource) (flowschema.Schema, bool) {
	declarationSource, _, declared := lspSchemaWorkspaceDeclaration(workspace, name)
	if !declared {
		return flowschema.Schema{}, false
	}
	entry := s.schemaCacheEntry(declarationSource.uri, declarationSource.source)
	for _, schema := range entry.schemas {
		if schema.Name == name {
			return schema, true
		}
	}
	return flowschema.Schema{}, false
}

func lspIdentifierEnd(text string, start int) int {
	if start >= len(text) || !isLSPIdentifierStart(text[start]) {
		return start
	}
	end := start + 1
	for end < len(text) && isLSPIdentifierByte(text[end]) {
		end++
	}
	return end
}

func isLSPIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isLSPIdentifierByte(value byte) bool {
	return isLSPIdentifierStart(value) || value >= '0' && value <= '9'
}

func lspPositionToByteOffset(source string, position lspPosition) (int, bool) {
	if position.Line < 0 || position.Character < 0 {
		return 0, false
	}
	lineStart := 0
	for currentLine := 0; ; currentLine++ {
		lineEnd := strings.IndexByte(source[lineStart:], '\n')
		lastLine := lineEnd < 0
		if lastLine {
			lineEnd = len(source)
		} else {
			lineEnd += lineStart
		}
		contentEnd := lineEnd
		if contentEnd > lineStart && source[contentEnd-1] == '\r' {
			contentEnd--
		}
		if currentLine == position.Line {
			character := 0
			for offset := lineStart; offset < contentEnd; {
				if character == position.Character {
					return offset, true
				}
				runeValue, size := utf8.DecodeRuneInString(source[offset:contentEnd])
				if size < 1 {
					size = 1
				}
				units := 1
				if runeValue > 0xffff {
					units = 2
				}
				if position.Character < character+units {
					return offset, true
				}
				character += units
				offset += size
			}
			if character == position.Character {
				return contentEnd, true
			}
			return 0, false
		}
		if lastLine {
			return 0, false
		}
		lineStart = lineEnd + 1
	}
}

func lspRangeForByteOffsets(source string, start, end int) lspRange {
	return lspRange{Start: lspPositionForByteOffset(source, start), End: lspPositionForByteOffset(source, end)}
}

func lspPositionForByteOffset(source string, offset int) lspPosition {
	offset = max(0, min(offset, len(source)))
	prefix := source[:offset]
	line := strings.Count(prefix, "\n")
	lineStart := strings.LastIndex(prefix, "\n") + 1
	columnText := strings.TrimSuffix(prefix[lineStart:], "\r")
	return lspPosition{Line: line, Character: utf16Length(columnText)}
}

func lspSchemaOccurrenceAt(index lspSchemaIndex, source string, position lspPosition) (lspSchemaOccurrence, bool) {
	offset, ok := lspPositionToByteOffset(source, position)
	if !ok {
		return lspSchemaOccurrence{}, false
	}
	for _, occurrence := range index.occurrences {
		if offset >= occurrence.start && offset < occurrence.end {
			return occurrence, true
		}
	}
	return lspSchemaOccurrence{}, false
}

func (s *flowLanguageServer) handleDefinition(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
		Position     *lspPosition               `json:"position"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.Position == nil {
		return s.invalidParams(message, errors.New("textDocument.uri and position are required"))
	}
	if len(message.ID) == 0 {
		return nil
	}
	uri := params.TextDocument.URI
	source, open := s.documents[uri]
	if !open {
		slog.Debug("LSP schema definition lookup", "uri", uri, "position", *params.Position, "documentOpen", false)
		return s.respond(message.ID, nil)
	}
	if location, found := s.pipelineDefinition(uri, source, *params.Position); found {
		return s.respond(message.ID, location)
	}
	workspace := s.schemaWorkspace(uri)
	current, found := lspSchemaWorkspaceSourceAt(workspace, uri)
	if !found {
		return s.respond(message.ID, nil)
	}
	occurrence, found := lspSchemaOccurrenceAt(current.index, source, *params.Position)
	if !found {
		slog.Debug("LSP schema definition lookup", "uri", uri, "position", *params.Position, "symbolFound", false)
		return s.respond(message.ID, nil)
	}
	declarationSource, declaration, declared := lspSchemaWorkspaceDeclaration(workspace, occurrence.name)
	if !declared {
		slog.Debug("LSP schema definition lookup", "uri", uri, "position", *params.Position, lspSourceKindSchema, occurrence.name, "declared", false)
		return s.respond(message.ID, nil)
	}
	slog.Debug("LSP schema definition resolved", "uri", uri, lspSourceKindSchema, occurrence.name, "declarationURI", declarationSource.uri, "line", lspPositionForByteOffset(declarationSource.source, declaration.start).Line+1)
	return s.respond(message.ID, map[string]any{
		"uri":       declarationSource.uri,
		lspRangeKey: lspRangeForByteOffsets(declarationSource.source, declaration.start, declaration.end),
	})
}

func (s *flowLanguageServer) handleReferences(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
		Position     *lspPosition               `json:"position"`
		Context      struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.Position == nil {
		return s.invalidParams(message, errors.New("textDocument.uri and position are required"))
	}
	if len(message.ID) == 0 {
		return nil
	}
	uri := params.TextDocument.URI
	source, open := s.documents[uri]
	if !open {
		slog.Debug("LSP schema references lookup", "uri", uri, "position", *params.Position, "documentOpen", false)
		return s.respond(message.ID, []any{})
	}
	if locations, found := s.pipelineReferences(uri, source, *params.Position, params.Context.IncludeDeclaration); found {
		return s.respond(message.ID, locations)
	}
	workspace := s.schemaWorkspace(uri)
	current, found := lspSchemaWorkspaceSourceAt(workspace, uri)
	if !found {
		return s.respond(message.ID, []any{})
	}
	occurrence, found := lspSchemaOccurrenceAt(current.index, source, *params.Position)
	if !found {
		slog.Debug("LSP schema references lookup", "uri", uri, "position", *params.Position, "symbolFound", false)
		return s.respond(message.ID, []any{})
	}
	if _, _, declared := lspSchemaWorkspaceDeclaration(workspace, occurrence.name); !declared {
		slog.Debug("LSP schema references lookup", "uri", uri, "position", *params.Position, lspSourceKindSchema, occurrence.name, "declared", false)
		return s.respond(message.ID, []any{})
	}
	locations := make([]map[string]any, 0)
	for i := range workspace {
		workspaceSource := &workspace[i]
		for _, match := range workspaceSource.index.occurrences {
			if match.name != occurrence.name || match.declaration && !params.Context.IncludeDeclaration {
				continue
			}
			locations = append(locations, map[string]any{
				"uri":       workspaceSource.uri,
				lspRangeKey: lspRangeForByteOffsets(workspaceSource.source, match.start, match.end),
			})
		}
	}
	slog.Debug("LSP schema references resolved", "uri", uri, lspSourceKindSchema, occurrence.name, "count", len(locations), "includeDeclaration", params.Context.IncludeDeclaration)
	return s.respond(message.ID, locations)
}

func (s *flowLanguageServer) handleSchemaRename(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
		Position     *lspPosition               `json:"position"`
		NewName      *string                    `json:"newName"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.Position == nil || params.NewName == nil {
		return s.invalidParams(message, errors.New("textDocument.uri, position, and newName are required"))
	}
	if !lspValidSchemaName(*params.NewName) {
		return s.invalidParams(message, xerr.Validation(fmt.Sprintf("newName %q is not a valid schema identifier", *params.NewName)))
	}
	if len(message.ID) == 0 {
		return nil
	}
	edit, found := s.schemaRenameEdit(params.TextDocument.URI, *params.Position, *params.NewName)
	if !found {
		return s.respond(message.ID, nil)
	}
	return s.respond(message.ID, edit)
}

func (s *flowLanguageServer) schemaRenameEdit(uri string, position lspPosition, newName string) (map[string]any, bool) {
	source, open := s.documents[uri]
	if !open {
		return nil, false
	}
	workspace := s.schemaWorkspace(uri)
	current, found := lspSchemaWorkspaceSourceAt(workspace, uri)
	if !found {
		return nil, false
	}
	selected, found := lspSchemaOccurrenceAt(current.index, source, position)
	if !found {
		return nil, false
	}
	if !lspSchemaHasUniqueDeclaration(workspace, selected.name) {
		return nil, false
	}
	if newName == selected.name {
		return map[string]any{lspChangesKey: map[string][]lspTextEdit{}}, true
	}
	if lspSchemaRenameNameConflicts(workspace, newName) {
		return nil, false
	}

	changes := make(map[string][]lspTextEdit)
	for i := range workspace {
		workspaceSource := &workspace[i]
		for _, occurrence := range workspaceSource.index.occurrences {
			if occurrence.name != selected.name {
				continue
			}
			changes[workspaceSource.uri] = append(changes[workspaceSource.uri], lspTextEdit{
				Range:   lspRangeForByteOffsets(workspaceSource.source, occurrence.start, occurrence.end),
				NewText: newName,
			})
		}
	}
	return map[string]any{lspChangesKey: changes}, true
}

func lspSchemaHasUniqueDeclaration(workspace []lspSchemaWorkspaceSource, name string) bool {
	declarationCount := 0
	for i := range workspace {
		for _, occurrence := range workspace[i].index.occurrences {
			if occurrence.declaration && occurrence.name == name {
				declarationCount++
			}
		}
	}
	return declarationCount == 1
}

func lspSchemaRenameNameConflicts(workspace []lspSchemaWorkspaceSource, newName string) bool {
	for i := range workspace {
		for _, occurrence := range workspace[i].index.occurrences {
			if occurrence.name == newName {
				// Do not create a collision or silently bind existing unresolved references.
				return true
			}
		}
	}
	return false
}

func lspValidSchemaName(name string) bool {
	return name != "" && lspIdentifierEnd(name, 0) == len(name)
}

func (s *flowLanguageServer) schemaHover(uri, source string, position lspPosition) (map[string]any, bool) {
	workspace := s.schemaWorkspace(uri)
	current, found := lspSchemaWorkspaceSourceAt(workspace, uri)
	if !found {
		return nil, false
	}
	occurrence, found := lspSchemaOccurrenceAt(current.index, source, position)
	if !found {
		return nil, false
	}
	declarationSource, declaration, declared := lspSchemaWorkspaceDeclaration(workspace, occurrence.name)
	if !declared {
		return nil, false
	}

	entry := s.schemaCacheEntry(declarationSource.uri, declarationSource.source)
	markdown := fmt.Sprintf("**%s** — NFlow schema", occurrence.name)
	for _, schema := range entry.schemas {
		if schema.Name == occurrence.name {
			markdown = formatLSPFlowSchemaHover(schema)
			slog.Debug("LSP schema hover resolved", "uri", uri, lspSourceKindSchema, occurrence.name, "declarationURI", declarationSource.uri, "fields", len(schema.Fields))
			break
		}
	}
	if declaration.start >= 0 {
		line := lspPositionForByteOffset(declarationSource.source, declaration.start).Line + 1
		if declarationSource.uri == uri {
			markdown += fmt.Sprintf("\n\nDeclared on line %d.", line)
		} else {
			markdown += fmt.Sprintf("\n\nDeclared in %#q on line %d.", declarationSource.uri, line)
		}
	}
	return map[string]any{
		"contents":  map[string]string{"kind": "markdown", "value": markdown},
		lspRangeKey: lspRangeForByteOffsets(source, occurrence.start, occurrence.end),
	}, true
}

func formatLSPFlowSchemaHover(schema flowschema.Schema) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "**%s** — NFlow schema\n\n", schema.Name)
	fmt.Fprintf(&builder, "**Fields (%d)**", len(schema.Fields))
	for _, field := range schema.Fields {
		fmt.Fprintf(&builder, "\n- **%s** (`%s`): `%s`", field.Name, field.JSONName, field.Type)
		var tags []string
		if value, ok := field.Tags["default"]; ok {
			tags = append(tags, "default "+fmt.Sprintf("%q", value))
		}
		if value := field.Tags["validate"]; value != "" {
			tags = append(tags, "validate "+fmt.Sprintf("%q", value))
		}
		if value := field.Tags["enum"]; value != "" {
			tags = append(tags, "enum "+fmt.Sprintf("%q", value))
		}
		if len(tags) > 0 {
			fmt.Fprintf(&builder, " — %s", strings.Join(tags, "; "))
		}
		if description := field.Tags["usage"]; description != "" {
			fmt.Fprintf(&builder, "\n  - %s", strings.ReplaceAll(description, "\n", " "))
		} else if description := field.Tags["description"]; description != "" {
			fmt.Fprintf(&builder, "\n  - %s", strings.ReplaceAll(description, "\n", " "))
		}
	}
	return builder.String()
}

func (s *flowLanguageServer) handleSchemaHover(uri string, position lspPosition) (map[string]any, bool) {
	source, open := s.documents[uri]
	if !open {
		return nil, false
	}
	return s.schemaHover(uri, source, position)
}

func (s *flowLanguageServer) schemaIssues(uri, source string) []lspSchemaIssue {
	entry := s.schemaCacheEntry(uri, source)
	references, deprecatedHeaders := lspSchemaReferencesFromIndex(entry.sourceIndex)
	if len(references) == 0 {
		return nil
	}

	workspace := s.schemaWorkspace(uri)
	if _, found := lspSchemaWorkspaceSourceAt(workspace, uri); !found {
		workspace = append(workspace, lspSchemaWorkspaceSource{
			uri: uri, source: source, index: entry.index,
		})
	}
	known := make(map[string]struct{})
	for i := range workspace {
		for name := range workspace[i].index.declarations {
			known[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)

	issues := make([]lspSchemaIssue, 0)
	for _, reference := range references {
		if reference.modifier == lspSourceKindSchema {
			if _, deprecated := deprecatedHeaders[reference.start]; deprecated {
				issues = append(issues, lspSchemaIssue{
					diagnostic: lspDiagnostic{
						Range:    lspRangeForByteOffsets(source, reference.start, reference.start+len(":"+reference.modifier)),
						Severity: 2,
						Code:     "schema.deprecated-modifier",
						Source:   lspSource,
						Message:  ":schema is deprecated in a pipeline header; use :request-schema=Name",
					},
					fixTitle: "Replace with :request-schema",
					fixStart: reference.start,
					fixEnd:   reference.start + len(":schema"),
					fixText:  ":request-schema",
				})
			}
		}
		if !reference.nameExists {
			code := "schema.missing-name"
			message := fmt.Sprintf("schema reference :%s is missing a schema name", reference.modifier)
			if reference.value != "" {
				code = "schema.invalid-reference"
				message = fmt.Sprintf("invalid schema reference %q; expected an identifier", reference.value)
			}
			issues = append(issues, lspSchemaIssue{
				diagnostic: lspDiagnostic{
					Range:    lspRangeForByteOffsets(source, reference.nameStart, reference.nameEnd),
					Severity: 1,
					Code:     code,
					Source:   lspSource,
					Message:  message,
				},
			})
			continue
		}
		if reference.modifier == lspSourceKindSchema && len(known) == 0 {
			continue
		}
		if _, declared := known[reference.name]; declared {
			continue
		}
		message := fmt.Sprintf("unknown schema %q referenced by :%s", reference.name, reference.modifier)
		issue := lspSchemaIssue{
			diagnostic: lspDiagnostic{
				Range:    lspRangeForByteOffsets(source, reference.nameStart, reference.nameEnd),
				Severity: 1,
				Code:     "schema.unknown-name",
				Source:   lspSource,
				Message:  message,
			},
		}
		if suggestion, found := lspClosestSchemaName(reference.name, names); found {
			issue.diagnostic.Message += fmt.Sprintf("; did you mean %q?", suggestion)
			issue.fixTitle = "Replace with " + suggestion
			issue.fixStart = reference.nameStart
			issue.fixEnd = reference.nameEnd
			issue.fixText = suggestion
		}
		issues = append(issues, issue)
	}
	return issues
}

func lspSchemaReferencesFromIndex(index compiler.SourceIndex) (references []lspSchemaReference, deprecatedHeaders map[int]struct{}) {
	deprecatedHeaders = make(map[int]struct{})
	for _, block := range index.Blocks {
		if block.Kind != lspSourceKindPipeline {
			continue
		}
		for _, reference := range index.References {
			if reference.Kind == lspSourceKindSchema && reference.Modifier == lspSourceKindSchema &&
				reference.ModifierSpan.Start >= block.HeaderSpan.Start && reference.ModifierSpan.End <= block.HeaderSpan.End {
				deprecatedHeaders[reference.ModifierSpan.Start] = struct{}{}
			}
		}
	}
	for _, reference := range index.References {
		if reference.Kind != lspSourceKindSchema {
			continue
		}
		references = append(references, lspSchemaReference{
			modifier:   reference.Modifier,
			start:      reference.ModifierSpan.Start,
			end:        reference.Span.End,
			name:       reference.Name,
			value:      reference.Value,
			nameStart:  reference.NameSpan.Start,
			nameEnd:    reference.NameSpan.End,
			nameExists: reference.HasName,
		})
	}
	return references, deprecatedHeaders
}

func lspClosestSchemaName(name string, candidates []string) (string, bool) {
	if name == "" || len(candidates) == 0 {
		return "", false
	}
	bestDistance := int(^uint(0) >> 1)
	best := ""
	tie := false
	for _, candidate := range candidates {
		distance := lspDamerauLevenshtein(name, candidate)
		if distance < bestDistance {
			bestDistance, best, tie = distance, candidate, false
		} else if distance == bestDistance {
			tie = true
		}
	}
	threshold := max(1, utf8.RuneCountInString(name)/4)
	if tie || bestDistance > threshold {
		return "", false
	}
	return best, true
}

func lspDamerauLevenshtein(left, right string) int {
	a, b := []rune(left), []rune(right)
	previousPrevious := make([]int, len(b)+1)
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, leftRune := range a {
		current[0] = i + 1
		for j, rightRune := range b {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
			if i > 0 && j > 0 && leftRune == b[j-1] && a[i-1] == rightRune {
				current[j+1] = min(current[j+1], previousPrevious[j-1]+1)
			}
		}
		previousPrevious, previous, current = previous, current, previousPrevious
	}
	return previous[len(b)]
}

func (s *flowLanguageServer) handleCodeAction(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
		Range        *lspRange                  `json:"range"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.Range == nil {
		return s.invalidParams(message, errors.New("textDocument.uri and range are required"))
	}
	if len(message.ID) == 0 {
		return nil
	}
	uri := params.TextDocument.URI
	source, open := s.documents[uri]
	if !open {
		return s.respond(message.ID, []any{})
	}
	return s.respond(message.ID, s.schemaCodeActions(uri, source, *params.Range))
}

func (s *flowLanguageServer) schemaCodeActions(uri, source string, requested lspRange) []map[string]any {
	actions := make([]map[string]any, 0)
	issues := s.schemaIssues(uri, source)
	for i := range issues {
		issue := &issues[i]
		if issue.fixTitle == "" || !lspRangesIntersect(issue.diagnostic.Range, requested) {
			continue
		}
		edit := map[string]any{
			lspChangesKey: map[string]any{
				uri: []map[string]any{{
					lspRangeKey:   lspRangeForByteOffsets(source, issue.fixStart, issue.fixEnd),
					lspNewTextKey: issue.fixText,
				}},
			},
		}
		actions = append(actions, map[string]any{
			"title":       issue.fixTitle,
			"kind":        "quickfix",
			"diagnostics": []lspDiagnostic{issue.diagnostic},
			"isPreferred": true,
			"edit":        edit,
		})
	}
	return actions
}

func lspRangesIntersect(left, right lspRange) bool {
	return lspPositionLessOrEqual(left.Start, right.End) && lspPositionLessOrEqual(right.Start, left.End)
}

func lspPositionLessOrEqual(left, right lspPosition) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Character <= right.Character
}
