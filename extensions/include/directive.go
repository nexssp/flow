package include

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow/compiler"
)

// Directive imports a parsed source module. The compiler owns source loading,
// cycle detection, declaration scopes and namespace resolution.
var Directive = compiler.Directive{
	Name:    "include",
	Example: `@include "shared/payments.nflow" AS billing`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	rawLine := req.Lines[req.I]
	keywordStart := strings.Index(rawLine, "@include")
	textStart := keywordStart + len("@include")
	for textStart < len(rawLine) && (rawLine[textStart] == ' ' || rawLine[textStart] == '\t') {
		textStart++
	}
	text := strings.TrimSpace(rawLine[textStart:])
	target, rest, targetStart, targetEnd, err := includePathWithSpan(text, pos)
	if err != nil {
		return compiler.DirectiveRes{Next: req.I + 1}, err
	}
	namespace := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	var namespaceSpan compiler.SourceSpan
	rest = strings.TrimSpace(rest)
	if rest != "" && !includeComment(rest) {
		fields := strings.Fields(rest)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "AS") {
			return compiler.DirectiveRes{Next: req.I + 1}, compiler.SourceError(pos, "@include: expected AS namespace after file path")
		}
		namespace = fields[1]
		asSearchStart := textStart + targetEnd
		asRelative := strings.Index(strings.ToUpper(rawLine[asSearchStart:]), strings.ToUpper(fields[0]))
		if asRelative >= 0 {
			restStart := asSearchStart + asRelative
			namespaceRelative := strings.Index(rawLine[restStart+len(fields[0]):], namespace)
			if namespaceRelative >= 0 {
				namespaceStart := restStart + len(fields[0]) + namespaceRelative
				namespaceSpan = compiler.SourceSpan{Start: req.Offset(req.I, namespaceStart), End: req.Offset(req.I, namespaceStart+len(namespace))}
			}
		}
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(rest, fields[0])), namespace))
		if rest != "" && !includeComment(rest) {
			return compiler.DirectiveRes{Next: req.I + 1}, compiler.SourceError(pos, "@include: unexpected text after namespace: %s", rest)
		}
	}
	if req.Import == nil && req.ImportAt == nil {
		return compiler.DirectiveRes{Next: req.I + 1}, compiler.SourceError(pos, "@include: source imports are not available in this preprocessing context")
	}
	targetSpan := compiler.SourceSpan{Start: req.Offset(req.I, textStart+targetStart), End: req.Offset(req.I, textStart+targetEnd)}
	var importErr error
	if req.ImportAt != nil {
		importErr = req.ImportAt(target, namespace, req.LineSpan(req.I), targetSpan, namespaceSpan)
	} else {
		importErr = req.Import(target, namespace)
	}
	if importErr != nil {
		return compiler.DirectiveRes{Next: req.I + 1}, importErr
	}
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

func includePathWithSpan(text string, pos compiler.Position) (target, rest string, start, end int, err error) {
	if text == "" || includeComment(text) {
		return "", "", 0, 0, compiler.SourceError(pos, "@include requires a file path")
	}
	if quote := text[0]; quote == '"' || quote == '\'' || quote == '`' {
		for i := 1; i < len(text); i++ {
			if text[i] == quote {
				if i == 1 {
					return "", "", 0, 0, compiler.SourceError(pos, "@include requires a file path")
				}
				return text[1:i], text[i+1:], 1, i, nil
			}
		}
		return "", "", 0, 0, compiler.SourceError(pos, "@include: unterminated quoted path")
	}
	if i := strings.IndexAny(text, " \t"); i >= 0 {
		return text[:i], text[i:], 0, i, nil
	}
	return text, "", 0, len(text), nil
}

func includeComment(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//")
}
