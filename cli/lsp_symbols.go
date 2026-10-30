package cli

import (
	"errors"
	"strings"

	"github.com/nexssp/flow/compiler"
)

const (
	lspSymbolKindFunction = 12
	lspSymbolKindStruct   = 23
)

func (s *flowLanguageServer) handleDocumentSymbol(message lspMessage) error {
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
	entry := s.schemaCacheEntry(uri, source)
	return s.respond(message.ID, lspDocumentSymbolsFromIndex(source, entry.sourceIndex))
}

func lspDocumentSymbols(source string) []map[string]any {
	return lspDocumentSymbolsFromIndex(source, lspSourceIndexFromText(source))
}

func lspDocumentSymbolsFromIndex(source string, index compiler.SourceIndex) []map[string]any {
	symbols := make([]map[string]any, 0, len(index.Declarations))
	for _, declaration := range index.Declarations {
		kind, detail, ok := lspDocumentSymbolKind(declaration.Kind)
		if !ok {
			continue
		}
		span := declaration.Span
		for _, block := range index.Blocks {
			if block.Kind == declaration.Kind && block.Span.Start == declaration.Span.Start {
				span = block.Span
				break
			}
		}
		name := declaration.Name
		if declaration.Kind == lspSourceKindPipeline {
			name = strings.TrimPrefix(name, "pipeline.")
		}
		symbols = append(symbols, lspDocumentSymbol(
			name, detail, kind, source, span.Start, span.End,
			declaration.NameSpan.Start, declaration.NameSpan.End,
		))
	}
	return symbols
}

func lspDocumentSymbolKind(kind string) (symbolKind int, detail string, ok bool) {
	switch kind {
	case lspSourceKindPipeline:
		return lspSymbolKindFunction, "Pipeline", true
	case lspSourceKindSchema:
		return lspSymbolKindStruct, "Schema", true
	default:
		return 0, "", false
	}
}

func lspDocumentSymbol(name, detail string, kind int, source string, start, end, selectionStart, selectionEnd int) map[string]any {
	return map[string]any{
		"name":           name,
		"detail":         detail,
		"kind":           kind,
		lspRangeKey:      lspRangeForByteOffsets(source, start, end),
		"selectionRange": lspRangeForByteOffsets(source, selectionStart, selectionEnd),
	}
}
