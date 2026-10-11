// Package symbols provides the @symbols directive, which declares a
// file-local symbol namespace visible only inside the compiling source.
// File-local namespaces shadow global namespaces of the same name for
// that one compilation and never leak into the process-global registry.
package symbols

import (
	"context"
	"strings"

	"github.com/nexssp/flow/compiler"
)

var Directive = compiler.Directive{
	Name: "symbols",
	Example: `@symbols routing {
  primary_queue: "billing-operations"
  max_retries:   "3"
}`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])

	rest := strings.TrimSpace(strings.TrimPrefix(line, "@symbols"))
	if rest == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols: expected NAME { ... }")
	}
	nameEnd := strings.IndexAny(rest, " \t{")
	if nameEnd <= 0 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols: expected NAME { ... }, got %q", rest)
	}
	name := rest[:nameEnd]
	if !validNamespaceName(name) {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols: invalid namespace name %q", name)
	}

	entries, next, err := compiler.ReadBlock(req.Lines, req.I)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@symbols: %v", err)
	}
	if len(entries) == 0 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols %s: namespace must declare at least one symbol", name)
	}

	symbols := make(map[string]any, len(entries))
	for _, entry := range entries {
		key, value, ok := splitSymbolEntry(entry)
		if !ok {
			return compiler.DirectiveRes{}, compiler.SourceError(pos,
				"@symbols %s: expected `name: value`, got %q", name, entry)
		}
		symbols[key] = value
	}

	namespaces, _ := req.Out["symbols"].(map[string]compiler.SymbolNamespace)
	if namespaces == nil {
		namespaces = make(map[string]compiler.SymbolNamespace, 1)
	}
	if _, dup := namespaces[name]; dup {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols: duplicate namespace %q", name)
	}
	namespaces[name] = compiler.SymbolNamespace{Name: name, Symbols: symbols}
	req.Out["symbols"] = namespaces

	return compiler.DirectiveRes{Next: next}, nil
}

func splitSymbolEntry(entry string) (key string, value any, ok bool) {
	colon := strings.IndexByte(entry, ':')
	if colon <= 0 {
		return "", nil, false
	}
	k := strings.TrimSpace(entry[:colon])
	if k == "" {
		return "", nil, false
	}
	raw := compiler.TrimQuotes(strings.TrimSpace(entry[colon+1:]))
	return k, raw, true
}

func validNamespaceName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && !isLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !isLetter(r) && !isDigit(r) {
			return false
		}
	}
	return true
}

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
func isDigit(r rune) bool  { return r >= '0' && r <= '9' }
