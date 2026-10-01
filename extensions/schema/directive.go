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

	schema := Schema{Name: name}
	seen := make(map[string]bool, len(body))

	for _, raw := range body {
		field, fieldErr := parseSchemaField(raw)
		if fieldErr != nil {
			return core.DirectiveRes{}, core.SourceError(pos, "@schema %s: %v", name, fieldErr)
		}
		if field.Name == "" {
			continue
		}
		if seen[field.Name] {
			return core.DirectiveRes{}, core.SourceError(pos, "@schema %s: duplicate field %q", name, field.Name)
		}
		seen[field.Name] = true
		schema.Fields = append(schema.Fields, *field)
	}

	if len(schema.Fields) == 0 {
		return core.DirectiveRes{}, core.SourceError(pos, "@schema %s: at least one field is required", name)
	}

	existing := SchemasFromMap(req.Out)
	for _, prior := range existing {
		if prior.Name == name {
			return core.DirectiveRes{}, core.SourceError(pos, "@schema %s: duplicate declaration", name)
		}
	}

	req.Out["schemas"] = append(existing, schema)
	return core.DirectiveRes{Next: next}, nil
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
	if len(parts) != 2 {
		return nil, fmt.Errorf("expected `Name Type`, got %q", line)
	}

	name, typeText := parts[0], parts[1]
	if !isValidSchemaName(name) {
		return nil, fmt.Errorf("invalid field name %q", name)
	}

	field := &Field{Name: name, Type: typeText, Tags: tags}
	classifyFieldType(field)

	if tags != nil {
		if jsonName, ok := tags["json"]; ok {
			field.JSONName = strings.Split(jsonName, ",")[0]
		}
	}
	if field.JSONName == "" {
		field.JSONName = lowerFirst(name)
	}
	return field, nil
}
