// Package symbols provides the @symbols directive, which declares a
// file-local symbol namespace visible only inside the compiling source.
// File-local namespaces shadow global namespaces of the same name for
// that one compilation and never leak into the process-global registry.
package symbols

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

var Directive = compiler.Directive{
	Name: "symbols",
	Example: `@symbols http_status {
  ok: int = 200
  created: int = 201
  base_url: string = "https://api.internal"
  timeout: duration = 5s
}

@symbols legacy { pending: "pending" }`,
	Handler:     handleDirective,
	OpaqueBlock: true,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])

	rest := strings.TrimSpace(strings.TrimPrefix(line, "@symbols"))
	if rest == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@symbols: expected NAME { ... }")
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

	block, err := compiler.ReadBlockWithSpans(req.Lines, req.I)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@symbols: %v", err)
	}
	if len(block.Entries) == 0 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols %s: namespace must declare at least one symbol", name)
	}

	symbols := make(map[string]any, len(block.Entries))
	for _, entry := range block.Entries {
		parts := compiler.SplitTopLevel(entry.Text, ';')
		searchFrom := 0
		for _, part := range parts {
			text := strings.TrimSpace(part)
			if text == "" {
				continue
			}
			rel := strings.Index(entry.Text[searchFrom:], text)
			rel = max(0, rel)
			entryPos := positionAt(req, entry.Span.Start+searchFrom+rel)
			searchFrom += rel + len(text) + 1
			if err := addEntry(entryPos, name, symbols, text); err != nil {
				return compiler.DirectiveRes{}, err
			}
		}
	}
	if len(symbols) == 0 {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols %s: namespace must declare at least one symbol", name)
	}

	namespaces, _ := req.Out["symbols"].(map[string]compiler.SymbolNamespace)
	if namespaces == nil {
		namespaces = make(map[string]compiler.SymbolNamespace, 1)
	}
	if err := req.DeclareIn("namespace", "symbols", name); err != nil {
		return compiler.DirectiveRes{}, err
	}
	if _, dup := namespaces[name]; dup {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@symbols: duplicate namespace %q", name)
	}

	namespaces[name] = compiler.SymbolNamespace{Name: name, Symbols: symbols}
	req.Out["symbols"] = namespaces
	return compiler.DirectiveRes{Next: block.Next}, nil
}

func addEntry(pos compiler.Position, namespace string, symbols map[string]any, entry string) error {
	if !strings.Contains(entry, ":") {
		for key := range strings.FieldsSeq(entry) {
			if err := addSymbol(pos, namespace, symbols, key, key); err != nil {
				return err
			}
		}
		return nil
	}

	key, rhs, ok := splitSymbolEntry(entry)
	if !ok {
		return compiler.SourceError(pos,
			"@symbols %s: expected bare symbol names or `name: value`, got %q", namespace, entry)
	}
	if !validNamespaceName(key) {
		return compiler.SourceError(pos, "@symbols %s: invalid symbol name %q", namespace, key)
	}
	if _, dup := symbols[key]; dup {
		return compiler.SourceError(pos, "@symbols %s: duplicate symbol %q", namespace, key)
	}
	var value any
	if equal := compiler.IndexTopLevel(rhs, '='); equal >= 0 {
		typeName := strings.TrimSpace(rhs[:equal])
		if isTypedSymbolType(typeName) {
			native, err := parseTypedLiteral(typeName, strings.TrimSpace(rhs[equal+1:]))
			if err != nil {
				return compiler.SourceError(pos, "@symbols %s: invalid %s literal for %q: %v", namespace, typeName, key, err)
			}
			value = compiler.TypedSymbolValue{Type: canonicalTypedSymbolType(typeName), Value: native}
		} else {
			value = compiler.TrimQuotes(rhs)
		}
	} else {
		value = compiler.TrimQuotes(rhs)
	}
	symbols[key] = value
	return nil
}

func addSymbol(pos compiler.Position, namespace string, symbols map[string]any, key string, value any) error {
	if !validNamespaceName(key) {
		return compiler.SourceError(pos,
			"@symbols %s: invalid symbol name %q", namespace, key)
	}
	if _, dup := symbols[key]; dup {
		return compiler.SourceError(pos,
			"@symbols %s: duplicate symbol %q", namespace, key)
	}
	symbols[key] = value
	return nil
}

func splitSymbolEntry(entry string) (key, value string, ok bool) {
	colon := strings.IndexByte(entry, ':')
	if colon <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(entry[:colon])
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(entry[colon+1:]), true
}

func parseTypedLiteral(typeName, raw string) (any, error) {
	if raw == "" {
		return nil, fmt.Errorf("typed %s value is empty", typeName)
	}
	switch typeName {
	case "string":
		if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
			return nil, errors.New("typed string literal must be double-quoted")
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid string literal: %w", err)
		}
		return value, nil
	case "int":
		return action.CoerceStringValue(raw, reflect.TypeFor[int]())
	case "float64":
		return action.CoerceStringValue(raw, reflect.TypeFor[float64]())
	case "bool":
		return action.CoerceStringValue(raw, reflect.TypeFor[bool]())
	case "duration", "time.Duration":
		return action.CoerceStringValue(raw, reflect.TypeFor[time.Duration]())
	default:
		return nil, fmt.Errorf("unsupported type %q", typeName)
	}
}

func isTypedSymbolType(typeName string) bool {
	switch typeName {
	case "string", "int", "float64", "bool", "duration", "time.Duration":
		return true
	default:
		return false
	}
}

func canonicalTypedSymbolType(typeName string) string {
	if typeName == "time.Duration" {
		return "duration"
	}
	return typeName
}

func positionAt(req compiler.DirectiveReq, offset int) compiler.Position {
	for line := range req.Lines {
		start := req.Offset(line, 0)
		end := req.Offset(line, len(req.Lines[line]))
		if offset >= start && offset <= end {
			return compiler.Position{File: req.File, Line: line + 1, Col: offset - start + 1}
		}
	}
	return compiler.Position{File: req.File, Line: req.I + 1}
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
