package schema

import (
	"context"
	"fmt"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// Directive parses `@schema NAME { ... }` into a Schema appended to
// meta["schemas"]. Duplicate names are a compile error.
var Directive = compiler.Directive{
	Name:        "schema",
	Example:     `@schema Plan { Summary string }`,
	Handler:     handleDirective,
	OpaqueBlock: true,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])
	rawLine := req.Lines[req.I]
	keywordStart := strings.Index(rawLine, "@schema")
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@schema"))

	before, _, ok := strings.Cut(rest, "{")
	if !ok {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@schema: expected `NAME struct { ... }` or `NAME { ... }`")
	}

	header := strings.TrimSpace(before)
	header = strings.TrimSuffix(header, "struct")
	name := strings.TrimSpace(header)

	if name == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@schema: name is required before '{'")
	}
	if !isValidSchemaName(name) {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@schema: invalid name %q", name)
	}

	openBrace := strings.IndexByte(rawLine, '{')
	nameStart := keywordStart + len("@schema")
	for nameStart < len(rawLine) && (rawLine[nameStart] == ' ' || rawLine[nameStart] == '\t') {
		nameStart++
	}
	if strings.HasPrefix(rawLine[nameStart:], name) {
		keywordSpan := compiler.SourceSpan{Start: req.Offset(req.I, keywordStart), End: req.Offset(req.I, keywordStart+len("@schema"))}
		nameSpan := compiler.SourceSpan{Start: req.Offset(req.I, nameStart), End: req.Offset(req.I, nameStart+len(name))}
		headerSpan := compiler.SourceSpan{Start: keywordSpan.Start, End: req.Offset(req.I, openBrace+1)}
		req.RecordDeclaration(compiler.SourceDeclaration{Kind: "schema", Name: name, KeywordSpan: keywordSpan, NameSpan: nameSpan, Span: headerSpan})
	}

	block, err := compiler.ReadBlockWithSpans(req.Lines, req.I)
	blockStart := req.Offset(req.I, keywordStart)
	req.RecordBlock(compiler.SourceBlock{
		Kind:       "schema",
		Span:       compiler.SourceSpan{Start: blockStart, End: block.Span.End},
		HeaderSpan: compiler.SourceSpan{Start: blockStart, End: req.Offset(req.I, openBrace+1)},
		BodySpan:   block.BodySpan,
	})
	if err != nil {
		return compiler.DirectiveRes{Next: block.Next}, compiler.SourceError(pos, "@schema %s: %v", name, err)
	}
	body, next := block.Body, block.Next

	existing := SchemasFromMap(req.Out)
	if declareErr := req.DeclareAt("schema", name,
		compiler.SourceSpan{Start: req.Offset(req.I, keywordStart), End: req.Offset(req.I, keywordStart+len("@schema"))},
		compiler.SourceSpan{Start: req.Offset(req.I, nameStart), End: req.Offset(req.I, nameStart+len(name))},
		compiler.SourceSpan{Start: req.Offset(req.I, keywordStart), End: req.Offset(req.I, openBrace+1)}); declareErr != nil {
		return compiler.DirectiveRes{Next: next}, declareErr
	}
	for _, prior := range existing {
		if prior.Name == name {
			return compiler.DirectiveRes{Next: next}, compiler.SourceError(pos, "@schema %s: duplicate declaration", name)
		}
	}

	raw, embedReferences, err := parseSchemaEntries(block.Entries, body, req, pos, name)
	if err != nil {
		return compiler.DirectiveRes{Next: next}, err
	}
	if resolveErr := resolveSchemaSymbols(raw, compiler.SymbolsFromMeta(req.Out)); resolveErr != nil {
		if _, positioned := compiler.PositionOf(resolveErr); positioned {
			return compiler.DirectiveRes{Next: next}, resolveErr
		}
		return compiler.DirectiveRes{Next: next}, compiler.SourceError(pos, "@schema %s: %v", name, resolveErr)
	}
	if defaultErr := validateSchemaDefaults(raw); defaultErr != nil {
		if _, positioned := compiler.PositionOf(defaultErr); positioned {
			return compiler.DirectiveRes{Next: next}, defaultErr
		}
		return compiler.DirectiveRes{Next: next}, compiler.SourceError(pos, "@schema %s: %v", name, defaultErr)
	}
	for _, reference := range embedReferences {
		req.RecordReference(reference)
	}

	fields, err := resolveEmbeds(raw, existing, pos, name)
	if err != nil {
		return compiler.DirectiveRes{Next: next}, err
	}
	if len(fields) == 0 {
		return compiler.DirectiveRes{Next: next}, compiler.SourceError(pos, "@schema %s: at least one field is required", name)
	}

	req.Out["schemas"] = append(existing, Schema{Name: name, Fields: fields})
	return compiler.DirectiveRes{Next: next}, nil
}

func parseSchemaEntries(entries []compiler.BlockEntry, body []string, req compiler.DirectiveReq, pos compiler.Position, schemaName string) ([]Field, []compiler.SourceReference, error) {
	fields := make([]Field, 0, len(body))
	var references []compiler.SourceReference
	for i, raw := range body {
		field, err := parseSchemaField(raw)
		if err != nil {
			fieldPos := pos
			if i < len(entries) {
				fieldPos = positionAt(req, entries[i].Span.Start)
			}
			return nil, nil, compiler.SourceError(fieldPos, "@schema %s: %v", schemaName, err)
		}
		if i < len(entries) {
			field.Position = positionAt(req, entries[i].Span.Start)
		}
		if field.Name == "" && field.Embed == "" {
			continue
		}
		fields = append(fields, *field)
		if field.Embed != "" && i < len(entries) && entries[i].Span.End > entries[i].Span.Start {
			span := compiler.SourceSpan{Start: entries[i].Span.Start, End: entries[i].Span.Start + len(field.Embed)}
			references = append(references, compiler.SourceReference{Kind: "schema-embed", Name: field.Embed, Span: span, NameSpan: span, HasName: true, Valid: true})
		}
	}
	return fields, references, nil
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

// resolveEmbeds flattens embedded schemas into the parent field list.
// Embedded schemas must be declared before use (Go-style ordering), so
// transitive cycles are impossible by construction; only self-embed and
// name collisions need explicit checks.
func resolveEmbeds(raw []Field, declared []Schema, pos compiler.Position, schemaName string) ([]Field, error) {
	byName := make(map[string]Schema, len(declared))
	for _, s := range declared {
		byName[s.Name] = s
	}

	out := make([]Field, 0, len(raw))
	seen := make(map[string]bool, len(raw))

	for i := range raw {
		f := raw[i]
		if f.Embed == "" {
			if seen[f.Name] {
				return nil, compiler.SourceError(pos, "@schema %s: duplicate field %q", schemaName, f.Name)
			}
			seen[f.Name] = true
			out = append(out, f)
			continue
		}

		if f.Embed == schemaName {
			return nil, compiler.SourceError(pos, "@schema %s: cannot embed itself", schemaName)
		}
		src, ok := byName[f.Embed]
		if !ok {
			return nil, compiler.SourceError(pos,
				"@schema %s: embedded schema %q is not declared before this point", schemaName, f.Embed)
		}
		for i := range src.Fields {
			embedded := src.Fields[i]
			if seen[embedded.Name] {
				return nil, compiler.SourceError(pos,
					"@schema %s: field %q collides with an embedded schema field", schemaName, embedded.Name)
			}
			seen[embedded.Name] = true
			out = append(out, embedded)
		}
	}
	return out, nil
}

// parseSchemaField parses a field line with optional struct tags.
func parseSchemaField(line string) (*Field, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return &Field{}, nil
	}

	var tags map[string]string
	if tagStart := strings.IndexByte(line, '`'); tagStart >= 0 {
		tagEnd := strings.LastIndexByte(line, '`')
		if tagEnd <= tagStart {
			return nil, fmt.Errorf("unclosed tag literal in %q", line)
		}
		parsed, err := parseStructTags(line[tagStart+1 : tagEnd])
		if err != nil {
			return nil, err
		}
		tags = parsed
		line = strings.TrimSpace(line[:tagStart])
	}

	parts := strings.Fields(line)
	switch len(parts) {
	case 1:
		if tags != nil {
			return nil, fmt.Errorf("embedded schema %q must not carry tags", parts[0])
		}
		if !isValidSchemaName(parts[0]) {
			return nil, fmt.Errorf("invalid embed name %q", parts[0])
		}
		return &Field{Embed: parts[0]}, nil
	case 2:
		name, typeText := parts[0], parts[1]
		if !isValidSchemaName(name) {
			return nil, fmt.Errorf("invalid field name %q", name)
		}
		field := &Field{Name: name, Type: typeText, Tags: tags}
		classifyFieldType(field)
		if jsonName, ok := tags["json"]; ok {
			field.JSONName = strings.Split(jsonName, ",")[0]
		}
		if field.JSONName == "" {
			field.JSONName = lowerFirst(name)
		}
		return field, nil
	default:
		return nil, fmt.Errorf("expected `Name Type` or `Embed`, got %q", line)
	}
}
