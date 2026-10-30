package cli

import (
	"context"
	"log/slog"
	"sync"

	"github.com/nexssp/flow/compiler"
	flowinclude "github.com/nexssp/flow/extensions/include"
	flowpipeline "github.com/nexssp/flow/extensions/pipeline"
	flowschema "github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/runner"
)

const (
	lspSourceKindSchema      = "schema"
	lspSourceKindPipeline    = "pipeline"
	lspSourceKindSchemaEmbed = "schema-embed"
)

var (
	lspIndexDirectiveOnce  sync.Once
	lspIndexDirectiveTable *compiler.DirectiveTable
)

// lspSourceIndexFromText is the source-only entry point used by small LSP
// adapters and tests. It delegates Flow syntax to its existing directive
// handlers and compiler lexer rather than maintaining a parallel grammar.
func lspSourceIndexFromText(source string) compiler.SourceIndex {
	index, _, err := compiler.IndexSourceWithMetadata(context.Background(), lspIndexDirectives(), source, "")
	if err != nil {
		slog.Debug("LSP source-only index contains parser errors", "error", err)
	}
	return index
}

func lspIndexDirectives() *compiler.DirectiveTable {
	lspIndexDirectiveOnce.Do(func() {
		lspIndexDirectiveTable = compiler.NewDirectiveTable(
			flowinclude.Directive,
			flowpipeline.Directive,
			flowpipeline.EndDirective,
			flowschema.Directive,
		)
	})
	return lspIndexDirectiveTable
}

func (s *flowLanguageServer) sourceIndexAndMetadata(uri, source string) (compiler.SourceIndex, map[string]any, *compiler.PreparedSource, runner.Config, error) {
	name := s.sourceName(uri)
	if s.inv != nil {
		prepared, config, err := s.inv.prepareRequiredPreparedSource(s.ctx, source, name)
		if err == nil {
			index := prepared.SourceIndex()
			metadata := prepared.Metadata()
			return index, metadata, prepared, config, nil
		}
		slog.Debug("LSP source preparation unavailable; using tolerant Flow index", "uri", uri, "error", err)
		index, metadata, indexErr := compiler.IndexSourceWithMetadata(s.ctx, lspIndexDirectives(), source, name)
		if indexErr != nil {
			slog.Debug("LSP tolerant source index contains parser errors", "uri", uri, "error", indexErr)
		}
		return index, metadata, nil, runner.Config{}, err
	}
	index, metadata, err := compiler.IndexSourceWithMetadata(s.ctx, lspIndexDirectives(), source, name)
	if err != nil {
		slog.Debug("LSP partial source index", "uri", uri, "error", err)
	}
	return index, metadata, nil, runner.Config{}, err
}
