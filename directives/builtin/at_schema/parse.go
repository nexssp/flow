package at_schema

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/directives/core"
)

// parseSchemaHeader extracts the schema name from `@schema NAME struct`.
func parseSchemaHeader(header string) (string, error) {
	rest, ok := core.StripDirectivePrefix(header, "schema")
	if !ok {
		return "", fmt.Errorf("malformed header: %q", header)
	}
	rest = strings.TrimSpace(rest)
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		return "", fmt.Errorf("expected `NAME struct`, got %q", rest)
	}
	if parts[1] != "struct" {
		return "", fmt.Errorf("expected `struct` keyword, got %q", parts[1])
	}
	if !isValidGoIdent(parts[0]) {
		return "", fmt.Errorf("invalid schema name %q (must be an exported Go identifier)", parts[0])
	}
	return parts[0], nil
}

// parseSchemaBody parses every field line in the block body.
func parseSchemaBody(name string, body []string, ctx *core.Context, baseLine int) (Schema, error) {
	schema := Schema{
		Name: name,
		Pos:  core.Position{File: ctx.File, Line: baseLine + 1},
	}
	seen := map[string]bool{}

	for offset, rawLine := range body {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		bodyLine := baseLine + offset + 2

		field, err := parseSchemaField(line, ctx, bodyLine, name)
		if err != nil {
			return Schema{}, err
		}
		if seen[field.Name] {
			return Schema{}, core.AtErrf(ctx, bodyLine, "schema "+name,
				"duplicate field %q", field.Name)
		}
		seen[field.Name] = true
		schema.Fields = append(schema.Fields, field)
	}

	if len(schema.Fields) == 0 {
		return Schema{}, core.AtErrf(ctx, baseLine, "schema "+name,
			"at least one field is required")
	}
	return schema, nil
}

// parseSchemaField parses `Name Type [\`tags\`]`.
func parseSchemaField(line string, ctx *core.Context, bodyLine int, schemaName string) (SchemaField, error) {
	var tags map[string]string

	if tagStart := strings.IndexByte(line, '`'); tagStart >= 0 {
		tagEnd := strings.LastIndexByte(line, '`')
		if tagEnd <= tagStart {
			return SchemaField{}, core.AtErrf(ctx, bodyLine, "schema "+schemaName,
				"unclosed tag literal in %q", line)
		}
		var err error
		tags, err = parseStructTags(line[tagStart+1 : tagEnd])
		if err != nil {
			return SchemaField{}, core.AtErrf(ctx, bodyLine, "schema "+schemaName,
				"invalid tags: %v", err)
		}
		line = strings.TrimSpace(line[:tagStart])
	}

	fields := strings.Fields(line)
	if len(fields) != 2 {
		return SchemaField{}, core.AtErrf(ctx, bodyLine, "schema "+schemaName,
			"expected `Name Type` (with optional tags), got %q", line)
	}

	fieldName, typeText := fields[0], fields[1]
	if !isValidGoIdent(fieldName) {
		return SchemaField{}, core.AtErrf(ctx, bodyLine, "schema "+schemaName,
			"invalid field name %q (must be exported)", fieldName)
	}

	field := SchemaField{
		Name: fieldName,
		Type: typeText,
		Tags: tags,
		Pos:  core.Position{File: ctx.File, Line: bodyLine},
	}
	classifyFieldType(&field)

	if tags != nil {
		if jn, ok := tags["json"]; ok {
			field.JSONName = strings.Split(jn, ",")[0]
		}
	}
	if field.JSONName == "" {
		field.JSONName = lowerCamel(fieldName)
	}

	return field, nil
}

// classifyFieldType fills Kind, ElemType, Pointer, Slice, and Map based
// on the raw type text.
func classifyFieldType(f *SchemaField) {
	t := strings.TrimSpace(f.Type)

	if strings.HasPrefix(t, "*") {
		f.Pointer = true
		t = strings.TrimSpace(t[1:])
	}

	if strings.HasPrefix(t, "[]") {
		f.Slice = true
		f.ElemType = strings.TrimSpace(t[2:])
		f.Kind = classifyScalarKind(f.ElemType)
		return
	}

	if strings.HasPrefix(t, "map[") {
		f.Map = true
		// map[K]V — we only care about V for schema purposes.
		// endIdx names the closing bracket; "close" would shadow
		// the predeclared builtin.
		if endIdx := strings.IndexByte(t, ']'); endIdx > 0 && endIdx < len(t)-1 {
			f.ElemType = strings.TrimSpace(t[endIdx+1:])
			f.Kind = classifyScalarKind(f.ElemType)
		} else {
			f.Kind = KindAny
		}
		return
	}

	f.Kind = classifyScalarKind(t)
	if f.Kind == KindStruct {
		f.ElemType = t
	}
}

// parseStructTags parses `key:"value" key:"value"`.
func parseStructTags(s string) (map[string]string, error) {
	out := map[string]string{}
	i := 0
	for i < len(s) {
		// skip whitespace
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}

		// read key
		keyStart := i
		for i < len(s) && s[i] != ':' && s[i] != ' ' {
			i++
		}
		if i >= len(s) || s[i] != ':' {
			return nil, fmt.Errorf("expected ':' after key at offset %d", keyStart)
		}
		key := s[keyStart:i]
		i++ // skip ':'

		// read quoted value
		if i >= len(s) || s[i] != '"' {
			return nil, fmt.Errorf("expected '\"' after key %q", key)
		}
		i++
		valStart := i
		for i < len(s) && s[i] != '"' {
			if s[i] == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			i++
		}
		if i >= len(s) {
			return nil, fmt.Errorf("unclosed value for tag %q", key)
		}
		val := s[valStart:i]
		i++ // skip closing quote

		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate tag %q", key)
		}
		out[key] = val
	}
	return out, nil
}
