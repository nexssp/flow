package compiler

import (
	"context"
	"fmt"
	"strings"
)

// SourceImport is a parsed source module, not a map of declarations merged into
// its caller. Its declarations and extension contributions stay file-local.
type SourceImport struct {
	Namespace string
	Position  Position
	Source    *PreparedSource
}

const sourceScopeKey = "compiler.source.scope"

type sourceScope struct {
	declarations map[string]Position
	imports      []SourceImport
}

// Declare records a declaration at the directive's source position. Handlers
// call it after parsing the name and before writing their compilation state.
// The compiler, rather than an include merger, owns duplicate diagnostics.
func (req DirectiveReq) Declare(kind, name string) error {
	return req.DeclareIn(kind, kind, name)
}

// DeclareIn registers a binding in a shared declaration space. The directive
// kind is only a diagnostic label; the compiler does not know extension kinds.
func (req DirectiveReq) DeclareIn(space, kind, name string) error {
	if req.Out == nil {
		return SourceError(Position{File: req.File, Line: req.I + 1}, "declaration output is unavailable")
	}
	scope, _ := req.Out[sourceScopeKey].(*sourceScope)
	if scope == nil {
		scope = &sourceScope{declarations: make(map[string]Position)}
		req.Out[sourceScopeKey] = scope
	}
	key := space + "\x00" + name
	pos := Position{File: req.File, Line: req.I + 1}
	if previous, exists := scope.declarations[key]; exists {
		return SourceError(pos, "duplicate declaration @%s %q; first declared at %s", kind, name, previous)
	}
	scope.declarations[key] = pos
	return nil
}

// SourceImports returns imports in source order. Returned module state is
// immutable after preprocessing and can be reused for multiple compilations.
func SourceImports(out map[string]any) []SourceImport {
	scope, _ := out[sourceScopeKey].(*sourceScope)
	if scope == nil {
		return nil
	}
	return append([]SourceImport(nil), scope.imports...)
}

// DeclarationPosition locates a declaration recorded by its parser.
func DeclarationPosition(out map[string]any, space, name string) (Position, bool) {
	scope, _ := out[sourceScopeKey].(*sourceScope)
	if scope == nil {
		return Position{}, false
	}
	key := space + "\x00" + name
	pos, ok := scope.declarations[key]
	return pos, ok
}

func validSourceNamespace(name string) bool {
	if name == "" || strings.ContainsAny(name, "./~*") {
		return false
	}
	lexer := NewLexer(name)
	token := lexer.Next()
	return token.Type == TokIdent && token.Lit == name && lexer.Next().Type == TokEOF
}

func importSource(ctx context.Context, dt *DirectiveTable, loader SourceLoader, stack *includeStack, req DirectiveReq, target, namespace string) error {
	pos := Position{File: req.File, Line: req.I + 1}
	if !validSourceNamespace(namespace) {
		return SourceError(pos, "@include: invalid namespace %q; use AS with an identifier", namespace)
	}
	if err := req.DeclareIn("namespace", "include", namespace); err != nil {
		return err
	}
	data, resolvedPath, loadErr := loader.Load(req.File, target)
	if loadErr != nil {
		return SourceError(pos, "@include: %w", loadErr)
	}
	if !stack.push(resolvedPath) {
		return SourceError(pos, "@include cycle detected: %s", stack.chain(resolvedPath))
	}
	defer stack.pop()
	childClean, childOut, childErr := preprocess(ctx, dt, string(data), resolvedPath, stack)
	if childErr != nil {
		return childErr
	}
	scope, _ := req.Out[sourceScopeKey].(*sourceScope)
	scope.imports = append(scope.imports, SourceImport{
		Namespace: namespace, Position: pos,
		Source: &PreparedSource{source: string(data), name: resolvedPath, clean: childClean, meta: childOut},
	})
	if hasPipelineBody(childClean) {
		req.Body[req.I] = namespace + ".__source__"
	}
	return nil
}

// DeclarationOwner labels a mounted declaration with its parsed source origin.
func DeclarationOwner(out map[string]any, kind, name, owner string) string {
	if pos, ok := DeclarationPosition(out, kind, name); ok {
		return fmt.Sprintf("%s (%s)", owner, pos)
	}
	return owner
}
