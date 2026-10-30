package cli

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

type lspPipelineSymbol struct {
	name       string
	actionName string
	start      int
	end        int
}

type lspPipelineTarget struct {
	uri         string
	source      string
	symbol      lspPipelineSymbol
	referenceAs string
}

type lspPipelineImport struct {
	childKey  string
	namespace string
}

func lspPipelineSymbols(source string) []lspPipelineSymbol {
	return lspPipelineSymbolsFromIndex(lspSourceIndexFromText(source))
}

func lspPipelineSymbolsFromIndex(index compiler.SourceIndex) []lspPipelineSymbol {
	symbols := make([]lspPipelineSymbol, 0)
	for _, declaration := range index.Declarations {
		if declaration.Kind != lspSourceKindPipeline || declaration.NameSpan.End <= declaration.NameSpan.Start {
			continue
		}
		symbols = append(symbols, lspPipelineSymbol{
			name: declaration.Name, actionName: lspPipelineActionName(declaration.Name),
			start: declaration.NameSpan.Start, end: declaration.NameSpan.End,
		})
	}
	return symbols
}

func lspPipelineActionName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return "pipeline." + name
}

func (s *flowLanguageServer) pipelineTargets(uri string) []lspPipelineTarget {
	return s.pipelineTargetsInGraph(uri, s.sourceGraph(uri))
}

func (s *flowLanguageServer) pipelineTargetsInGraph(uri string, workspace []lspSchemaWorkspaceSource) []lspPipelineTarget {
	if len(workspace) == 0 {
		return nil
	}

	sourcesByKey := make(map[string]*lspSchemaWorkspaceSource, len(workspace))
	for i := range workspace {
		source := &workspace[i]
		sourcesByKey[lspSchemaSourceKey(source.uri)] = source
	}
	rootKey := lspSchemaSourceKey(uri)
	if _, exists := sourcesByKey[rootKey]; !exists {
		return nil
	}

	importsByKey := make(map[string][]lspPipelineImport)
	for i := range workspace {
		source := &workspace[i]
		parentKey := lspSchemaSourceKey(source.uri)
		for _, imported := range source.sourceIndex.Imports {
			if imported.Source == nil || imported.Namespace == "" {
				continue
			}
			childName, _ := imported.Source.SourceIdentity()
			childKey := lspPipelineImportedSourceKey(s, source.uri, childName)
			if _, exists := sourcesByKey[childKey]; !exists {
				continue
			}
			importsByKey[parentKey] = append(importsByKey[parentKey], lspPipelineImport{
				childKey: childKey, namespace: imported.Namespace,
			})
		}
	}

	var targets []lspPipelineTarget
	visited := make(map[string]struct{})
	var visit func(string, string)
	visit = func(sourceKey, prefix string) {
		visitKey := sourceKey + "\x00" + prefix
		if _, exists := visited[visitKey]; exists {
			return
		}
		visited[visitKey] = struct{}{}
		source, exists := sourcesByKey[sourceKey]
		if !exists {
			return
		}
		for _, symbol := range lspPipelineSymbolsFromIndex(source.sourceIndex) {
			targets = append(targets, lspPipelineTarget{
				uri:         source.uri,
				source:      source.source,
				symbol:      symbol,
				referenceAs: prefix + symbol.actionName,
			})
		}
		for _, imported := range importsByKey[sourceKey] {
			visit(imported.childKey, prefix+imported.namespace+".")
		}
	}
	visit(rootKey, "")
	return targets
}

func lspPipelineImportedSourceKey(s *flowLanguageServer, parentURI, childName string) string {
	_, key := lspPipelineImportedSourceURI(s, parentURI, childName)
	return key
}

func lspPipelineImportedSourceURI(s *flowLanguageServer, parentURI, childName string) (uri, key string) {
	childPath := lspFileURIPath(childName)
	if childPath == "" {
		childPath = childName
	}
	if !filepath.IsAbs(childPath) {
		parentPath := s.sourceName(parentURI)
		if !filepath.IsAbs(parentPath) {
			return "", ""
		}
		childPath = filepath.Join(filepath.Dir(parentPath), childPath)
	}
	childPath = lspSchemaPathKey(childPath)
	childURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(childPath)}).String()
	return childURI, lspSchemaSourceKey(childURI)
}

func (s *flowLanguageServer) sourceGraph(uri string) []lspSchemaWorkspaceSource {
	rootSource, exists := s.documents[uri]
	if !exists {
		return nil
	}
	openByPath := make(map[string]string, len(s.documents))
	for openURI := range s.documents {
		if path := lspFileURIPath(openURI); path != "" {
			openByPath[lspSchemaPathKey(path)] = openURI
		}
	}
	rootEntry := s.schemaCacheEntry(uri, rootSource)
	sources := make([]lspSchemaWorkspaceSource, 0, 4)
	seen := make(map[string]struct{})
	var add func(string, string, compiler.SourceIndex, bool)
	add = func(sourceURI, source string, sourceIndex compiler.SourceIndex, open bool) {
		key := lspSchemaSourceKey(sourceURI)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		sources = append(sources, lspSchemaWorkspaceSource{
			uri: sourceURI, source: source, sourceIndex: sourceIndex,
			index: lspSchemaIndexFromSourceIndex(sourceIndex), open: open,
		})
		for _, imported := range sourceIndex.Imports {
			if imported.Source == nil {
				continue
			}
			childName, _ := imported.Source.SourceIdentity()
			childURI, childKey := lspPipelineImportedSourceURI(s, sourceURI, childName)
			if childKey == "" {
				continue
			}
			if openURI, isOpen := openByPath[lspSchemaPathKey(lspFileURIPath(childURI))]; isOpen {
				childSource := s.documents[openURI]
				childEntry := s.schemaCacheEntry(openURI, childSource)
				add(openURI, childSource, childEntry.sourceIndex, true)
				continue
			}
			add(childURI, imported.Source.SourceText(), imported.Source.SourceIndex(), false)
		}
	}
	add(uri, rootSource, rootEntry.sourceIndex, true)
	return sources
}

func lspPipelineReferenceAt(index compiler.SourceIndex, offset int) (name string, start, end int, ok bool) {
	if offset < 0 {
		return "", 0, 0, false
	}
	for _, reference := range index.References {
		if reference.Kind != "identifier" {
			continue
		}
		if offset >= reference.NameSpan.Start && offset < reference.NameSpan.End {
			return reference.Name, reference.NameSpan.Start, reference.NameSpan.End, true
		}
	}
	return "", 0, 0, false
}

func (s *flowLanguageServer) pipelineSelection(uri, source string, position lspPosition) (target lspPipelineTarget, start, end int, ok bool) {
	offset, ok := lspPositionToByteOffset(source, position)
	if !ok {
		return lspPipelineTarget{}, 0, 0, false
	}
	index := s.schemaCacheEntry(uri, source).sourceIndex
	targets := s.pipelineTargets(uri)
	currentDeclarations := lspPipelineSymbolsFromIndex(index)
	for _, declaration := range currentDeclarations {
		if offset < declaration.start || offset >= declaration.end {
			continue
		}
		matches := lspPipelineTargetsByDeclaration(targets, uri, declaration)
		if len(matches) != 1 {
			return lspPipelineTarget{}, 0, 0, false
		}
		return matches[0], declaration.start, declaration.end, true
	}

	token, start, end, found := lspPipelineReferenceAt(index, offset)
	if !found {
		return lspPipelineTarget{}, 0, 0, false
	}
	matches := make([]lspPipelineTarget, 0, 1)
	for _, target := range targets {
		if target.referenceAs == token {
			matches = append(matches, target)
		}
	}
	matches = lspUniquePipelineTargets(matches)
	if len(matches) != 1 {
		return lspPipelineTarget{}, 0, 0, false
	}
	return matches[0], start, end, true
}

func lspPipelineTargetsByDeclaration(targets []lspPipelineTarget, uri string, declaration lspPipelineSymbol) []lspPipelineTarget {
	matches := make([]lspPipelineTarget, 0, 1)
	for _, target := range targets {
		if target.uri == uri && target.symbol.start == declaration.start && target.symbol.end == declaration.end {
			matches = append(matches, target)
		}
	}
	return lspUniquePipelineTargets(matches)
}

func lspUniquePipelineTargets(targets []lspPipelineTarget) []lspPipelineTarget {
	unique := make([]lspPipelineTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		key := fmt.Sprintf("%s\x00%d\x00%d\x00%s", target.uri, target.symbol.start, target.symbol.end, target.referenceAs)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, target)
	}
	return unique
}

func (s *flowLanguageServer) pipelineDefinition(uri, source string, position lspPosition) (map[string]any, bool) {
	target, _, _, found := s.pipelineSelection(uri, source, position)
	if !found {
		return nil, false
	}
	return map[string]any{
		"uri":       target.uri,
		lspRangeKey: lspRangeForByteOffsets(target.source, target.symbol.start, target.symbol.end),
	}, true
}

func (s *flowLanguageServer) pipelineHover(uri, source string, position lspPosition) (map[string]any, bool) {
	target, start, end, found := s.pipelineSelection(uri, source, position)
	if !found {
		return nil, false
	}
	line := lspPositionForByteOffset(target.source, target.symbol.start).Line + 1
	markdown := fmt.Sprintf("**%s** — Flow pipeline action\n\nDeclared as `@pipeline %s` on line %d.", target.referenceAs, target.symbol.name, line)
	if target.uri != uri {
		markdown = fmt.Sprintf("**%s** — Flow pipeline action\n\nDeclared as %#q in %#q on line %d.", target.referenceAs, "@pipeline "+target.symbol.name, target.uri, line)
	}
	return map[string]any{
		"contents":  map[string]string{"kind": "markdown", "value": markdown},
		lspRangeKey: lspRangeForByteOffsets(source, start, end),
	}, true
}

type lspPipelineIdentifier struct {
	name  string
	start int
	end   int
}

type lspPipelineLocation struct {
	uri    string
	source string
	start  int
	end    int
}

func lspPipelineIdentifiers(index compiler.SourceIndex) []lspPipelineIdentifier {
	var identifiers []lspPipelineIdentifier
	for _, reference := range index.References {
		if reference.Kind != "identifier" {
			continue
		}
		identifiers = append(identifiers, lspPipelineIdentifier{
			name: reference.Name, start: reference.NameSpan.Start, end: reference.NameSpan.End,
		})
	}
	return identifiers
}

func lspSamePipelineTarget(left, right lspPipelineTarget) bool {
	return lspSchemaSourceKey(left.uri) == lspSchemaSourceKey(right.uri) &&
		left.symbol.start == right.symbol.start && left.symbol.end == right.symbol.end
}

func lspPipelineTargetsByReference(targets []lspPipelineTarget, name string) []lspPipelineTarget {
	matches := make([]lspPipelineTarget, 0, 1)
	for _, target := range targets {
		if target.referenceAs == name {
			matches = append(matches, target)
		}
	}
	return lspUniquePipelineTargets(matches)
}

func lspPipelineDeclarationAt(symbols []lspPipelineSymbol, start, end int) bool {
	for _, symbol := range symbols {
		if symbol.start == start && symbol.end == end {
			return true
		}
	}
	return false
}

func (s *flowLanguageServer) pipelineReferences(uri, source string, position lspPosition, includeDeclaration bool) ([]map[string]any, bool) {
	selected, _, _, found := s.pipelineSelection(uri, source, position)
	if !found {
		return nil, false
	}

	var locations []lspPipelineLocation
	seen := make(map[string]struct{})
	addLocation := func(location lspPipelineLocation) {
		key := fmt.Sprintf("%s\x00%d\x00%d", lspSchemaSourceKey(location.uri), location.start, location.end)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		locations = append(locations, location)
	}
	if includeDeclaration {
		addLocation(lspPipelineLocation{
			uri: selected.uri, source: selected.source,
			start: selected.symbol.start, end: selected.symbol.end,
		})
	}

	workspace := s.sourceGraph(uri)
	for i := range workspace {
		workspaceSource := &workspace[i]
		targets := s.pipelineTargetsInGraph(workspaceSource.uri, workspace)
		aliases := make(map[string]struct{})
		for _, target := range targets {
			if lspSamePipelineTarget(target, selected) {
				aliases[target.referenceAs] = struct{}{}
			}
		}
		if len(aliases) == 0 {
			continue
		}
		declarations := lspPipelineSymbolsFromIndex(workspaceSource.sourceIndex)
		for _, identifier := range lspPipelineIdentifiers(workspaceSource.sourceIndex) {
			if _, isAlias := aliases[identifier.name]; !isAlias || lspPipelineDeclarationAt(declarations, identifier.start, identifier.end) {
				continue
			}
			matches := lspPipelineTargetsByReference(targets, identifier.name)
			if len(matches) != 1 || !lspSamePipelineTarget(matches[0], selected) {
				continue
			}
			addLocation(lspPipelineLocation{
				uri: workspaceSource.uri, source: workspaceSource.source,
				start: identifier.start, end: identifier.end,
			})
		}
	}

	sort.Slice(locations, func(i, j int) bool {
		if locations[i].uri != locations[j].uri {
			return locations[i].uri < locations[j].uri
		}
		return locations[i].start < locations[j].start
	})
	result := make([]map[string]any, 0, len(locations))
	for _, location := range locations {
		result = append(result, map[string]any{
			"uri":       location.uri,
			lspRangeKey: lspRangeForByteOffsets(location.source, location.start, location.end),
		})
	}
	return result, true
}

func (s *flowLanguageServer) handleRename(message lspMessage) error {
	handled, err := s.handlePipelineRename(message)
	if handled || err != nil {
		return err
	}
	return s.handleSchemaRename(message)
}

func (s *flowLanguageServer) handlePipelineRename(message lspMessage) (bool, error) {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
		Position     *lspPosition               `json:"position"`
		NewName      *string                    `json:"newName"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return false, err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.Position == nil || params.NewName == nil || len(message.ID) == 0 {
		return false, nil
	}
	uri := params.TextDocument.URI
	source, open := s.documents[uri]
	if !open {
		return false, nil
	}
	selected, _, _, found := s.pipelineSelection(uri, source, *params.Position)
	if !found {
		return false, nil
	}
	if !lspValidPipelineRenameName(*params.NewName) {
		return true, s.invalidParams(message, xerr.Validation(fmt.Sprintf("newName %q is not a safe Flow pipeline identifier", *params.NewName)))
	}
	edit, safe := s.pipelineRenameEdit(uri, selected, *params.NewName)
	if !safe {
		return true, s.respond(message.ID, nil)
	}
	return true, s.respond(message.ID, edit)
}

func lspValidPipelineRenameName(name string) bool {
	if name == "" {
		return false
	}
	for segment := range strings.SplitSeq(name, ".") {
		if segment == "" || !lspPipelineNameStart(segment[0]) {
			return false
		}
		for i := 1; i < len(segment); i++ {
			if !lspPipelineNameStart(segment[i]) && (segment[i] < '0' || segment[i] > '9') && segment[i] != '-' {
				return false
			}
		}
	}
	return true
}

func lspPipelineNameStart(char byte) bool {
	return char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func (s *flowLanguageServer) pipelineRenameEdit(uri string, selected lspPipelineTarget, newName string) (map[string]any, bool) {
	workspace := s.sourceGraph(uri)
	if !lspPipelineHasUniqueDeclaration(workspace, selected) {
		return nil, false
	}
	if newName == selected.symbol.name {
		return map[string]any{lspChangesKey: map[string][]lspTextEdit{}}, true
	}

	changes := make(map[string][]lspTextEdit)
	changes[selected.uri] = append(changes[selected.uri], lspTextEdit{
		Range:   lspRangeForByteOffsets(selected.source, selected.symbol.start, selected.symbol.end),
		NewText: newName,
	})

	for i := range workspace {
		workspaceSource := &workspace[i]
		edits, safe := s.pipelineRenameEditsForSource(workspaceSource, workspace, selected, lspPipelineActionName(newName))
		if !safe {
			return nil, false
		}
		if len(edits) > 0 {
			changes[workspaceSource.uri] = append(changes[workspaceSource.uri], edits...)
		}
	}

	for editURI, edits := range changes {
		sort.Slice(edits, func(i, j int) bool {
			left, right := edits[i].Range.Start, edits[j].Range.Start
			return left.Line > right.Line || left.Line == right.Line && left.Character > right.Character
		})
		changes[editURI] = edits
	}
	return map[string]any{lspChangesKey: changes}, true
}

func lspPipelineHasUniqueDeclaration(workspace []lspSchemaWorkspaceSource, selected lspPipelineTarget) bool {
	declarationCount := 0
	for i := range workspace {
		workspaceSource := &workspace[i]
		if lspSchemaSourceKey(workspaceSource.uri) != lspSchemaSourceKey(selected.uri) {
			continue
		}
		for _, declaration := range lspPipelineSymbolsFromIndex(workspaceSource.sourceIndex) {
			if declaration.start == selected.symbol.start && declaration.end == selected.symbol.end {
				declarationCount++
			}
		}
	}
	return declarationCount == 1
}

func (s *flowLanguageServer) pipelineRenameEditsForSource(workspaceSource *lspSchemaWorkspaceSource, workspace []lspSchemaWorkspaceSource, selected lspPipelineTarget, newActionName string) ([]lspTextEdit, bool) {
	targets := s.pipelineTargetsInGraph(workspaceSource.uri, workspace)
	aliases := make(map[string]string)
	for _, target := range targets {
		if !lspSamePipelineTarget(target, selected) || !strings.HasSuffix(target.referenceAs, target.symbol.actionName) {
			continue
		}
		prefix := strings.TrimSuffix(target.referenceAs, target.symbol.actionName)
		aliases[target.referenceAs] = prefix + newActionName
	}
	if len(aliases) == 0 {
		return nil, true
	}

	declarations := lspPipelineSymbolsFromIndex(workspaceSource.sourceIndex)
	identifiers := lspPipelineIdentifiers(workspaceSource.sourceIndex)
	if !lspPipelineRenameSourceIsSafe(targets, aliases, declarations, identifiers, selected) {
		return nil, false
	}

	edits := make([]lspTextEdit, 0)
	for _, identifier := range identifiers {
		newReference, isReference := aliases[identifier.name]
		if !isReference || lspPipelineDeclarationAt(declarations, identifier.start, identifier.end) {
			continue
		}
		matches := lspPipelineTargetsByReference(targets, identifier.name)
		if len(matches) != 1 || !lspSamePipelineTarget(matches[0], selected) {
			return nil, false
		}
		if newReference != identifier.name {
			edits = append(edits, lspTextEdit{
				Range:   lspRangeForByteOffsets(workspaceSource.source, identifier.start, identifier.end),
				NewText: newReference,
			})
		}
	}
	return edits, true
}

func lspPipelineRenameSourceIsSafe(targets []lspPipelineTarget, aliases map[string]string, declarations []lspPipelineSymbol, identifiers []lspPipelineIdentifier, selected lspPipelineTarget) bool {
	for _, newReference := range aliases {
		if !lspPipelineRenameTargetAvailable(targets, newReference, selected) {
			return false
		}
		for _, identifier := range identifiers {
			if identifier.name != newReference || lspPipelineDeclarationAt(declarations, identifier.start, identifier.end) {
				continue
			}
			matches := lspPipelineTargetsByReference(targets, identifier.name)
			if len(matches) != 1 || !lspSamePipelineTarget(matches[0], selected) {
				return false
			}
		}
	}
	return true
}

func lspPipelineRenameTargetAvailable(targets []lspPipelineTarget, reference string, selected lspPipelineTarget) bool {
	for _, target := range targets {
		if target.referenceAs == reference && !lspSamePipelineTarget(target, selected) {
			return false
		}
	}
	return true
}
