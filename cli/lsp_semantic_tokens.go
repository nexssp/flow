package cli

import (
	"errors"
	"sort"
)

const (
	lspSemanticTokenKeyword = iota
	lspSemanticTokenFunction
	lspSemanticTokenClass
)

const lspSemanticTokenDeclaration = 1 << iota

var (
	lspSemanticTokenTypes     = []string{"keyword", "function", "class"}
	lspSemanticTokenModifiers = []string{"declaration"}
)

type lspSemanticToken struct {
	start     int
	end       int
	tokenType int
	modifiers int
}

func (s *flowLanguageServer) handleSemanticTokensFull(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" {
		return s.invalidParams(message, errors.New("textDocument.uri is required"))
	}
	if len(message.ID) == 0 {
		return nil
	}
	uri := params.TextDocument.URI
	source, exists := s.documents[uri]
	if !exists {
		return s.invalidParams(message, errors.New("document is not open"))
	}
	return s.respond(message.ID, map[string]any{"data": s.semanticTokenData(uri, source)})
}

func (s *flowLanguageServer) semanticTokenData(uri, source string) []int {
	entry := s.schemaCacheEntry(uri, source)
	index := entry.sourceIndex
	tokens := make([]lspSemanticToken, 0, len(index.Declarations)+len(index.References))
	for _, declaration := range index.Declarations {
		var tokenType int
		switch declaration.Kind {
		case lspSourceKindSchema:
			tokenType = lspSemanticTokenClass
		case lspSourceKindPipeline:
			tokenType = lspSemanticTokenFunction
		default:
			continue
		}
		tokens = append(tokens,
			lspSemanticToken{start: declaration.KeywordSpan.Start, end: declaration.KeywordSpan.End, tokenType: lspSemanticTokenKeyword},
			lspSemanticToken{start: declaration.NameSpan.Start, end: declaration.NameSpan.End, tokenType: tokenType, modifiers: lspSemanticTokenDeclaration},
		)
	}
	for _, reference := range index.References {
		if reference.Kind != lspSourceKindSchema && reference.Kind != lspSourceKindSchemaEmbed || !reference.HasName {
			continue
		}
		tokens = append(tokens, lspSemanticToken{
			start: reference.NameSpan.Start, end: reference.NameSpan.End,
			tokenType: lspSemanticTokenClass,
		})
	}

	sort.SliceStable(tokens, func(i, j int) bool {
		if tokens[i].start != tokens[j].start {
			return tokens[i].start < tokens[j].start
		}
		return tokens[i].end < tokens[j].end
	})

	data := make([]int, 0, len(tokens)*5)
	previousLine, previousCharacter := 0, 0
	for _, token := range tokens {
		if token.start < 0 || token.end <= token.start || token.end > len(source) {
			continue
		}
		start := lspPositionForByteOffset(source, token.start)
		length := utf16Length(source[token.start:token.end])
		if length == 0 {
			continue
		}
		deltaLine := start.Line - previousLine
		deltaCharacter := start.Character
		if deltaLine == 0 {
			deltaCharacter -= previousCharacter
		}
		data = append(data, deltaLine, deltaCharacter, length, token.tokenType, token.modifiers)
		previousLine, previousCharacter = start.Line, start.Character
	}
	return data
}
