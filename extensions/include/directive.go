package include

import (
	"context"
	"path/filepath"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// Directive includes a source relative to the current file. The existing
// preprocessor owns recursion and cycle detection; each contributing directive
// owns the rules for merging its metadata. Include knows no metadata keys.
var Directive = compiler.Directive{
	Name:    "include",
	Example: `@include "shared/child.nflow"`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	target := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "@include")), `"'`)
	position := compiler.Position{File: req.File, Line: req.I + 1}
	if target == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(position, "@include requires a file path")
	}
	if !filepath.IsAbs(target) && req.BaseDir != "" {
		target = filepath.Join(req.BaseDir, target)
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(position, "@include: %v", err)
	}
	if req.Recurse == nil {
		return compiler.DirectiveRes{}, compiler.SourceError(position, "@include: recurse is not available in this preprocessing context")
	}
	clean, includedMeta, err := req.Recurse(target)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	if err := req.Table.MergeMetadata(req, includedMeta); err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(position, "@include: %v", err)
	}
	req.EmbedSource(clean, includedMeta)
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}
