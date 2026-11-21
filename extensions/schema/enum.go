package schema

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/internal/identifier"
	"github.com/nexssp/flow/internal/lexical"
)

// EnumMember is one named member of a file-local closed enum.
type EnumMember struct {
	Name  string
	Value string
}

// Enum is a file-local set of wire strings. Its name is qualified by an
// @include alias when exposed to another source module.
type Enum struct {
	Name    string
	Members []EnumMember
}

// EnumDirective parses @enum declarations and exposes their wire strings to
// the current source's expression environment.
var EnumDirective = compiler.Directive{
	Name:        "enum",
	Example:     "@enum OrderStatus { pending, paid = \"settled\" }",
	Handler:     handleEnumDirective,
	OpaqueBlock: true,
}

func handleEnumDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])
	rawLine := req.Lines[req.I]
	keywordStart := strings.Index(rawLine, "@enum")
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@enum"))
	before, _, ok := strings.Cut(rest, "{")
	if !ok {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@enum: expected `NAME { ... }`")
	}
	name := strings.TrimSpace(before)
	if name == "" || !identifier.IsValid(name) {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@enum: invalid or missing name %q", name)
	}

	openBrace := strings.IndexByte(rawLine, '{')
	nameStart := keywordStart + len("@enum")
	for nameStart < len(rawLine) && (rawLine[nameStart] == ' ' || rawLine[nameStart] == '\t') {
		nameStart++
	}
	keywordSpan := compiler.SourceSpan{Start: req.Offset(req.I, keywordStart), End: req.Offset(req.I, keywordStart+len("@enum"))}
	nameSpan := compiler.SourceSpan{Start: req.Offset(req.I, nameStart), End: req.Offset(req.I, nameStart+len(name))}
	headerSpan := compiler.SourceSpan{Start: keywordSpan.Start, End: req.Offset(req.I, openBrace+1)}
	req.RecordDeclaration(compiler.SourceDeclaration{Kind: "enum", Name: name, KeywordSpan: keywordSpan, NameSpan: nameSpan, Span: headerSpan})

	block, err := compiler.ReadBlockWithSpans(req.Lines, req.I)
	blockStart := req.Offset(req.I, keywordStart)
	req.RecordBlock(compiler.SourceBlock{
		Kind:       "enum",
		Span:       compiler.SourceSpan{Start: blockStart, End: block.Span.End},
		HeaderSpan: headerSpan,
		BodySpan:   block.BodySpan,
	})
	if err != nil {
		return compiler.DirectiveRes{Next: block.Next}, compiler.SourceError(pos, "@enum %s: %v", name, err)
	}
	if len(block.Entries) == 0 {
		return compiler.DirectiveRes{Next: block.Next}, compiler.SourceError(pos, "@enum %s: at least one member is required", name)
	}
	if validationErr := validateEnumDeclaration(name, SchemasFromMap(req.Out), EnumsFromMap(req.Out)); validationErr != nil {
		return compiler.DirectiveRes{Next: block.Next}, compiler.SourceError(pos, "%v", validationErr)
	}
	members, err := parseEnumMembers(name, block.Entries, req)
	if err != nil {
		return compiler.DirectiveRes{Next: block.Next}, err
	}
	enum := Enum{Name: name, Members: members}

	if err := req.DeclareInAt("namespace", "enum", name, keywordSpan, nameSpan, headerSpan); err != nil {
		return compiler.DirectiveRes{Next: block.Next}, err
	}
	enums := append(EnumsFromMap(req.Out), enum)
	req.Out["enums"] = enums

	namespaces, _ := req.Out["symbols"].(map[string]compiler.SymbolNamespace)
	if namespaces == nil {
		namespaces = make(map[string]compiler.SymbolNamespace, 1)
	}
	symbolMembers := make(map[string]any, len(enum.Members))
	for _, member := range enum.Members {
		symbolMembers[member.Name] = member.Value
	}
	namespaces[name] = compiler.SymbolNamespace{Name: name, Symbols: symbolMembers}
	req.Out["symbols"] = namespaces
	return compiler.DirectiveRes{Next: block.Next}, nil
}

func validateEnumDeclaration(name string, schemas []Schema, enums []Enum) error {
	for schemaIndex := range schemas {
		schema := &schemas[schemaIndex]
		if schema.Name == name {
			return fmt.Errorf("@enum %s: name conflicts with a schema", name)
		}
		for fieldIndex := range schema.Fields {
			field := &schema.Fields[fieldIndex]
			fieldType := strings.TrimSpace(field.Type)
			if field.Pointer {
				fieldType = strings.TrimSpace(strings.TrimPrefix(fieldType, "*"))
			}
			if field.Slice {
				fieldType = strings.TrimSpace(strings.TrimPrefix(fieldType, "[]"))
			}
			fieldType = strings.TrimSpace(strings.TrimPrefix(fieldType, "*"))
			if fieldType == name {
				return fmt.Errorf("@enum %s: declared after @schema %s field %q; declare the enum first", name, schema.Name, field.Name)
			}
		}
	}
	for enumIndex := range enums {
		if enums[enumIndex].Name == name {
			return fmt.Errorf("@enum %s: duplicate declaration", name)
		}
	}
	return nil
}

func parseEnumMembers(name string, entries []compiler.BlockEntry, req compiler.DirectiveReq) ([]EnumMember, error) {
	members := make([]EnumMember, 0, len(entries))
	values := make(map[string]bool, len(entries))
	wireValues := make(map[string]bool, len(entries))
	for entryIndex := range entries {
		entry := entries[entryIndex]
		entryPos := req.PositionAt(entry.Span.Start)
		memberName, wireValue, explicit, err := parseEnumMember(entry.Text)
		if err != nil {
			return nil, compiler.SourceError(entryPos, "@enum %s: %v", name, err)
		}
		if !identifier.IsValid(memberName) {
			return nil, compiler.SourceError(entryPos, "@enum %s: invalid member name %q", name, memberName)
		}
		if values[memberName] {
			return nil, compiler.SourceError(entryPos, "@enum %s: duplicate member %q", name, memberName)
		}
		if !explicit {
			wireValue = memberName
		}
		if wireValue == "" {
			return nil, compiler.SourceError(entryPos, "@enum %s.%s: wire value must not be empty", name, memberName)
		}
		if strings.IndexFunc(wireValue, unicode.IsSpace) >= 0 {
			return nil, compiler.SourceError(entryPos, "@enum %s.%s: wire value must not contain whitespace", name, memberName)
		}
		if strings.ContainsRune(wireValue, ',') {
			return nil, compiler.SourceError(entryPos, "@enum %s.%s: wire value must not contain a comma", name, memberName)
		}
		if wireValues[wireValue] {
			return nil, compiler.SourceError(entryPos, "@enum %s: duplicate wire value %q", name, wireValue)
		}
		values[memberName] = true
		wireValues[wireValue] = true
		members = append(members, EnumMember{Name: memberName, Value: wireValue})
	}
	return members, nil
}

func parseEnumMember(text string) (name, wireValue string, explicit bool, err error) {
	equal := compiler.IndexTopLevel(text, '=')
	if equal < 0 {
		return strings.TrimSpace(text), "", false, nil
	}
	name = strings.TrimSpace(text[:equal])
	raw := strings.TrimSpace(text[equal+1:])
	if name == "" || raw == "" || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", "", false, fmt.Errorf("expected `member = \"wire-value\"`, got %q", text)
	}
	wireValue, err = lexical.DecodeDoubleQuotedString(raw)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid wire string for %q: %w", name, err)
	}
	return name, wireValue, true, nil
}

func EnumsFromMap(meta map[string]any) []Enum {
	enums, _ := meta["enums"].([]Enum)
	return enums
}

// VisibleEnumsFromMeta returns local enums and explicitly imported enums,
// qualified by each module's @include alias.
func VisibleEnumsFromMeta(meta map[string]any) []Enum {
	visible := append([]Enum(nil), EnumsFromMap(meta)...)
	for _, imported := range compiler.SourceImports(meta) {
		if imported.Source == nil {
			continue
		}
		for _, target := range VisibleEnumsFromMeta(imported.Source.Metadata()) {
			target.Name = imported.Namespace + "." + target.Name
			visible = append(visible, target)
		}
	}
	return visible
}

func resolveEnumFieldTypes(fields []Field, enums []Enum) error {
	byName := make(map[string]Enum, len(enums))
	for _, enum := range enums {
		byName[enum.Name] = enum
	}
	for i := range fields {
		field := &fields[i]
		if field.Map {
			continue
		}
		typeName := strings.TrimSpace(field.Type)
		if field.Pointer {
			typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "*"))
		}
		if field.Slice {
			typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "[]"))
		}
		enum, ok := byName[typeName]
		if !ok {
			if aliasErr := missingEnumAlias(typeName, enums); aliasErr != nil {
				return fieldSourceError(field, "%v", aliasErr)
			}
			continue
		}
		field.EnumType = enum.Name
		field.EnumValues = make([]string, len(enum.Members))
		for memberIndex, member := range enum.Members {
			field.EnumValues[memberIndex] = member.Value
		}
		field.Kind = KindString
		if err := resolveEnumFieldTags(field, enum, enums); err != nil {
			return err
		}
	}
	return nil
}

func missingEnumAlias(typeName string, enums []Enum) error {
	last := typeName
	if index := strings.LastIndexByte(last, '.'); index >= 0 {
		last = last[index+1:]
	}
	for _, enum := range enums {
		if dot := strings.LastIndexByte(enum.Name, '.'); dot >= 0 && enum.Name[dot+1:] == last {
			return fmt.Errorf("included enum %q must be referenced through its @include alias as %q", enum.Name, enum.Name)
		}
	}
	return nil
}

func resolveEnumFieldTags(field *Field, enum Enum, enums []Enum) error {
	if field.Tags == nil {
		return nil
	}
	if raw, exists := field.Tags["default"]; exists {
		member, err := resolveEnumMemberReference(raw, enum, enums, "default")
		if err != nil {
			return fieldSourceError(field, "field %q default: %v", field.Name, err)
		}
		field.Tags["default"] = member.Value
	}
	if raw, exists := field.Tags["validate"]; exists {
		rules := strings.Split(raw, ",")
		resolvedRules := make([]string, 0, len(rules)+1)
		inDive := false
		for _, rule := range rules {
			if strings.TrimSpace(rule) == "dive" {
				inDive = true
				resolvedRules = append(resolvedRules, rule)
				continue
			}
			name, operand, hasOperand := strings.Cut(rule, "=")
			if !hasOperand || strings.TrimSpace(name) != "oneof" {
				resolvedRules = append(resolvedRules, rule)
				continue
			}
			words := strings.Fields(operand)
			if len(words) == 0 {
				return fieldSourceError(field, "field %q validate: oneof for enum %q must name at least one member", field.Name, enum.Name)
			}
			values := make([]string, len(words))
			for wordIndex, reference := range words {
				member, err := resolveEnumMemberReference(reference, enum, enums, "oneof")
				if err != nil {
					return fieldSourceError(field, "field %q validate: %v", field.Name, err)
				}
				values[wordIndex] = member.Value
			}
			if field.Slice && !inDive {
				resolvedRules = append(resolvedRules, "dive")
				inDive = true
			}
			resolvedRules = append(resolvedRules, "oneof="+strings.Join(values, " "))
		}
		field.Tags["validate"] = strings.Join(resolvedRules, ",")
	}
	return nil
}

func resolveEnumMemberReference(reference string, expected Enum, enums []Enum, use string) (EnumMember, error) {
	dot := strings.LastIndexByte(reference, '.')
	if dot <= 0 || dot == len(reference)-1 {
		return EnumMember{}, fmt.Errorf("%s for enum %q must reference a member as %s.member", use, expected.Name, expected.Name)
	}
	enumName, memberName := reference[:dot], reference[dot+1:]
	var foundEnum *Enum
	for index := range enums {
		if enums[index].Name == enumName {
			foundEnum = &enums[index]
			break
		}
	}
	if foundEnum == nil {
		return EnumMember{}, fmt.Errorf("unknown enum member reference %q for enum %q", reference, expected.Name)
	}
	if foundEnum.Name != expected.Name {
		return EnumMember{}, fmt.Errorf("enum member %q belongs to %q, not %q", reference, foundEnum.Name, expected.Name)
	}
	for _, member := range foundEnum.Members {
		if member.Name == memberName {
			return member, nil
		}
	}
	return EnumMember{}, fmt.Errorf("unknown member %q of enum %q", memberName, expected.Name)
}
