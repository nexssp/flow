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
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Lines[req.I]), "@include"))
	target, rest, err := includePath(text, pos)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	namespace := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	rest = strings.TrimSpace(rest)
	if rest != "" && !includeComment(rest) {
		fields := strings.Fields(rest)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "AS") {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@include: expected AS namespace after file path")
		}
		namespace = fields[1]
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(rest, fields[0])), namespace))
		if rest != "" && !includeComment(rest) {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@include: unexpected text after namespace: %s", rest)
		}
	}
	if req.Import == nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@include: source imports are not available in this preprocessing context")
	}
	if err := req.Import(target, namespace); err != nil {
		return compiler.DirectiveRes{}, err
	}
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

func includePath(text string, pos compiler.Position) (target, rest string, err error) {
	if text == "" || includeComment(text) {
		return "", "", compiler.SourceError(pos, "@include requires a file path")
	}
	if quote := text[0]; quote == '"' || quote == '\'' || quote == '`' {
		for i := 1; i < len(text); i++ {
			if text[i] == quote {
				if i == 1 {
					return "", "", compiler.SourceError(pos, "@include requires a file path")
				}
				return text[1:i], text[i+1:], nil
			}
		}
		return "", "", compiler.SourceError(pos, "@include: unterminated quoted path")
	}
	if i := strings.IndexAny(text, " \t"); i >= 0 {
		return text[:i], text[i:], nil
	}
	return text, "", nil
}

func includeComment(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//")
}
