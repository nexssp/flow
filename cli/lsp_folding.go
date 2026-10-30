package cli

import (
	"errors"

	"github.com/nexssp/flow/compiler"
)

func (s *flowLanguageServer) handleFoldingRange(message lspMessage) error {
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
	source, exists := s.documents[params.TextDocument.URI]
	if !exists {
		return s.invalidParams(message, errors.New("document is not open"))
	}
	entry := s.schemaCacheEntry(params.TextDocument.URI, source)
	return s.respond(message.ID, lspFoldingRangesFromIndex(source, entry.sourceIndex))
}

func lspFoldingRanges(source string) []map[string]any {
	return lspFoldingRangesFromIndex(source, lspSourceIndexFromText(source))
}

func lspFoldingRangesFromIndex(source string, index compiler.SourceIndex) []map[string]any {
	ranges := make([]map[string]any, 0, len(index.Blocks))
	for _, block := range index.Blocks {
		if block.Span.End <= block.Span.Start {
			continue
		}
		startLine := lspPositionForByteOffset(source, block.Span.Start).Line
		endLine := lspPositionForByteOffset(source, block.Span.End-1).Line
		if endLine > startLine {
			ranges = append(ranges, lspFoldingRange(startLine, endLine))
		}
	}
	return ranges
}

func lspFoldingRange(startLine, endLine int) map[string]any {
	return map[string]any{
		"startLine": startLine,
		"endLine":   endLine,
		"kind":      "region",
	}
}
