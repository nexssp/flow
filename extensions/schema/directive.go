package schema

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/flow/core"
)

// Directive parses `@schema NAME { ... }` into a Schema appended to
// meta["schemas"]. Duplicate names are a compile error.
var Directive = core.Directive{
	Name:    "schema",
	Example: `@schema Plan { Summary string }`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@schema"))

	before, _, ok := strings.Cut(rest, "{")
	if !ok {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@schema: expected `NAME struct { ... }` or `NAME { ... }`")
	}

	header := strings.TrimSpace(before)
	header = strings.TrimSuffix(header, "struct")
	name := strings.TrimSpace(header)

	if name == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@schema: name is required before '{'")
	}
	if !isValidSchemaName(name) {
		return core.DirectiveRes{}, core.SourceError(pos, "@schema: invalid name %q", name)
	}

	body, next, err := core.ReadBlock(req.Lines, req.I)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos, "@schema %s: %v", name, err)
	}

	existing := SchemasFromMap(req.Out)
	for _, prior := range existing {
		if prior.Name == name {
			return core.DirectiveRes{}, core.SourceError(pos, "@schema %s: duplicate declaration", name)
		}
	}

	raw, err := parseSchemaBody(body, pos, name)
	if err != nil {
		return core.DirectiveRes{}, err
	}

	fields, err := resolveEmbeds(raw, existing, pos, name)
	if err != nil {
		return core.DirectiveRes{}, err
	}
	if len(fields) == 0 {
		return core.DirectiveRes{}, core.SourceError(pos, "@schema %s: at least one field is required", name)
	}

	req.Out["schemas"] = append(existing, Schema{Name: name, Fields: fields})
	return core.DirectiveRes{Next: next}, nil
}

func parseSchemaBody(body []string, pos core.Position, schemaName string) ([]Field, error) {
	fields := make([]Field, 0, len(body))
	for _, raw := range body {
		field, err := parseSchemaField(raw)
		if err != nil {
			return nil, core.SourceError(pos, "@schema %s: %v", schemaName, err)
		}
		if field.Name == "" && field.Embed == "" {
			continue
		}
		fields = append(fields, *field)
	}
	return fields, nil
}

// resolveEmbeds flattens embedded schemas into the parent field list.
// Embedded schemas must be declared before use (Go-style ordering), so
// transitive cycles are impossible by construction; only self-embed and
// name collisions need explicit checks.
func resolveEmbeds(raw []Field, declared []Schema, pos core.Position, schemaName string) ([]Field, error) {
	byName := make(map[string]Schema, len(declared))
	for _, s := range declared {
		byName[s.Name] = s
	}

	out := make([]Field, 0, len(raw))
	seen := make(map[string]bool, len(raw))

	for _, f := range raw {
		if f.Embed == "" {
			if seen[f.Name] {
				return nil, core.SourceError(pos, "@schema %s: duplicate field %q", schemaName, f.Name)
			}
			seen[f.Name] = true
			out = append(out, f)
			continue
		}

		if f.Embed == schemaName {
			return nil, core.SourceError(pos, "@schema %s: cannot embed itself", schemaName)
		}
		src, ok := byName[f.Embed]
		if !ok {
			return nil, core.SourceError(pos,
				"@schema %s: embedded schema %q is not declared before this point", schemaName, f.Embed)
		}
		for _, embedded := range src.Fields {
			if seen[embedded.Name] {
				return nil, core.SourceError(pos,
					"@schema %s: field %q collides with an embedded schema field", schemaName, embedded.Name)
			}
			seen[embedded.Name] = true
			out = append(out, embedded)
		}
	}
	return out, nil
}

// parseSchemaField parses one field line: `Name Type `tags“. Returns
// nil for a blank line.
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
