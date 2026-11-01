package funcs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/projection"
)

const functionMetadataKey = "nflow_functions"

// FunctionDeclaration is the source-level representation stored by @func.
type FunctionDeclaration struct {
	Name       string
	Parameters []projection.FunctionParameter
	Result     string
	Body       string
	Position   compiler.Position
}

// Directive parses one pure, single-expression function declaration per line.
// Its signature follows Go's name-then-type convention; only the body is
// expression syntax, not a Go statement block.
var Directive = compiler.Directive{
	Name:    "func",
	Example: "@func commerce.shippingFeeMinor(subtotalMinor int, destinationZone string) int = subtotalMinor >= 5000 && destinationZone == \"domestic\" ? 0 : 599",
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1, Col: 1}
	rawLine := req.Lines[req.I]
	line := strings.TrimSpace(rawLine)
	if !strings.HasPrefix(line, "@func") || len(line) > len("@func") && !isSpace(line[len("@func")]) {
		return compiler.DirectiveRes{Next: req.I + 1}, compiler.SourceError(pos, "@func: expected `@func namespace.name(params) result = expression`")
	}
	declaration, err := parseDeclaration(strings.TrimSpace(strings.TrimPrefix(line, "@func")))
	if err != nil {
		return compiler.DirectiveRes{Next: req.I + 1}, compiler.SourceError(pos, "@func: %v", err)
	}
	namePosition := compiler.Position{File: req.File, Line: req.I + 1, Col: strings.Index(rawLine, declaration.Name) + 1}
	declaration.Position = namePosition
	if previous, exists := findFunction(functionsFromMeta(req.Out), declaration.Name); exists {
		return compiler.DirectiveRes{Next: req.I + 1}, compiler.SourceError(namePosition,
			"@func %s: duplicate declaration; first declared at %s", declaration.Name, previous.Position)
	}

	keywordStart := strings.Index(rawLine, "@func")
	keywordSpan := compiler.SourceSpan{Start: req.Offset(req.I, keywordStart), End: req.Offset(req.I, keywordStart+len("@func"))}
	nameSpan := compiler.SourceSpan{Start: req.Offset(req.I, namePosition.Col-1), End: req.Offset(req.I, namePosition.Col-1+len(declaration.Name))}
	fullSpan := compiler.SourceSpan{Start: keywordSpan.Start, End: req.LineSpan(req.I).End}
	if err := req.DeclareAt("func", declaration.Name, keywordSpan, nameSpan, fullSpan); err != nil {
		return compiler.DirectiveRes{Next: req.I + 1}, err
	}

	declarations := append(functionsFromMeta(req.Out), declaration)
	req.Out[functionMetadataKey] = declarations
	return compiler.DirectiveRes{Next: req.I + 1, BlankLines: []int{req.I}}, nil
}

func parseDeclaration(raw string) (FunctionDeclaration, error) {
	open := strings.IndexByte(raw, '(')
	if open <= 0 {
		return FunctionDeclaration{}, errors.New("expected a qualified function name followed by `(`")
	}
	name := strings.TrimSpace(raw[:open])
	if !validQualifiedName(name) {
		return FunctionDeclaration{}, fmt.Errorf("invalid function name %q; use namespace.name", name)
	}
	closeIndex := strings.IndexByte(raw[open+1:], ')')
	if closeIndex < 0 {
		return FunctionDeclaration{}, errors.New("missing `)` after parameters")
	}
	closeIndex += open + 1
	parameters, err := parseParameters(raw[open+1 : closeIndex])
	if err != nil {
		return FunctionDeclaration{}, err
	}
	tail := strings.TrimSpace(raw[closeIndex+1:])
	resultText, bodyText, hasExpression := strings.Cut(tail, "=")
	if !hasExpression {
		return FunctionDeclaration{}, errors.New("expected `= expression` after the result type")
	}
	result := strings.TrimSpace(resultText)
	if !supportedType(result) {
		return FunctionDeclaration{}, fmt.Errorf("unsupported result type %q (supported: int, int64, float64, string, bool)", result)
	}
	body := strings.TrimSpace(bodyText)
	if body == "" {
		return FunctionDeclaration{}, errors.New("function body expression is required")
	}
	return FunctionDeclaration{Name: name, Parameters: parameters, Result: result, Body: body}, nil
}

func parseParameters(raw string) ([]projection.FunctionParameter, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	parameters := make([]projection.FunctionParameter, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) != 2 {
			return nil, errors.New("each parameter must be written `name type`")
		}
		name, typeName := fields[0], fields[1]
		if !validIdentifier(name) {
			return nil, fmt.Errorf("invalid parameter name %q", name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate parameter %q", name)
		}
		if !supportedType(typeName) {
			return nil, fmt.Errorf("unsupported parameter type %q for %q (supported: int, int64, float64, string, bool)", typeName, name)
		}
		seen[name] = struct{}{}
		parameters = append(parameters, projection.FunctionParameter{Name: name, Type: typeName})
	}
	return parameters, nil
}

func functionsFromMeta(meta map[string]any) []FunctionDeclaration {
	declarations, _ := meta[functionMetadataKey].([]FunctionDeclaration)
	return declarations
}

func findFunction(declarations []FunctionDeclaration, name string) (FunctionDeclaration, bool) {
	for _, declaration := range declarations {
		if declaration.Name == name {
			return declaration, true
		}
	}
	return FunctionDeclaration{}, false
}

func validQualifiedName(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !validIdentifier(part) {
			return false
		}
	}
	return true
}

func validIdentifier(name string) bool {
	if name == "" || !isIdentifierStart(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isIdentifierContinue(name[i]) {
			return false
		}
	}
	return true
}

func isIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isIdentifierContinue(value byte) bool {
	return isIdentifierStart(value) || value >= '0' && value <= '9'
}

func isSpace(value byte) bool { return value == ' ' || value == '\t' }

func supportedType(name string) bool {
	switch name {
	case "int", "int64", "float64", "string", "bool":
		return true
	default:
		return false
	}
}

func inheritFunctions(parentMeta, childMeta map[string]any) error {
	parent := visibleFunctionDefinitions(parentMeta)
	if len(parent) == 0 {
		return nil
	}
	declarations := make([]FunctionDeclaration, len(parent))
	for i, definition := range parent {
		declarations[i] = FunctionDeclaration{
			Name: definition.Name, Parameters: slices.Clone(definition.Parameters),
			Result: definition.Result, Body: definition.Body, Position: definition.Position,
		}
	}
	childMeta[functionMetadataKey] = declarations
	return nil
}

func functionSetFromMeta(meta map[string]any) *projection.FunctionSet {
	definitions := visibleFunctionDefinitions(meta)
	if len(definitions) == 0 {
		return nil
	}
	return projection.NewFunctionSet(definitions)
}

func visibleFunctionDefinitions(meta map[string]any) []projection.FunctionDefinition {
	definitions, _ := collectVisibleDefinitions(meta, "")
	return definitions
}

// collectVisibleDefinitions mounts a module's local functions and recursively
// imported functions beneath each @include namespace. Bodies are rewritten
// using the module's own visible names, so private calls keep working after
// each nesting level is mounted.
func collectVisibleDefinitions(meta map[string]any, prefix string) (definitions []projection.FunctionDefinition, visibleNames map[string]string) {
	local := functionsFromMeta(meta)
	visibleNames = make(map[string]string, len(local))
	localDefinitions := make([]projection.FunctionDefinition, 0, len(local))
	for _, declaration := range local {
		visibleNames[declaration.Name] = prefix + declaration.Name
		localDefinitions = append(localDefinitions, projection.FunctionDefinition{
			Name: prefix + declaration.Name, Parameters: slices.Clone(declaration.Parameters),
			Result: declaration.Result, Body: declaration.Body, Position: declaration.Position,
		})
	}

	var importedDefinitions []projection.FunctionDefinition
	for _, imported := range compiler.SourceImports(meta) {
		if imported.Source == nil {
			continue
		}
		childDefinitions, childVisibleNames := collectVisibleDefinitions(imported.Source.Metadata(), prefix+imported.Namespace+".")
		for childName, mountedName := range childVisibleNames {
			visibleNames[imported.Namespace+"."+childName] = mountedName
		}
		importedDefinitions = append(importedDefinitions, childDefinitions...)
	}
	for i := range localDefinitions {
		localDefinitions[i].Body = projection.RewriteFunctionReferences(localDefinitions[i].Body, visibleNames)
	}
	definitions = localDefinitions
	definitions = append(definitions, importedDefinitions...)
	return definitions, visibleNames
}
