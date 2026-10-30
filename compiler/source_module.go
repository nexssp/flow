package compiler

import (
	"context"
	"fmt"
	"strings"
)

// SourceImport is a parsed source module, not a map of declarations merged into
// its caller. Its declarations and extension contributions stay file-local.
type SourceImport struct {
	Namespace     string
	Position      Position
	Span          SourceSpan
	TargetSpan    SourceSpan
	NamespaceSpan SourceSpan
	Source        *PreparedSource
}

const sourceScopeKey = "compiler.source.scope"

type sourceScope struct {
	declarations map[string]Position
	imports      []SourceImport
	index        SourceIndex
}

// Declare records a declaration at the directive's source position. Handlers
// call it after parsing the name and before writing their compilation state.
// The compiler, rather than an include merger, owns duplicate diagnostics.
func (req DirectiveReq) Declare(kind, name string) error {
	return req.DeclareAt(kind, name, SourceSpan{}, SourceSpan{}, SourceSpan{})
}

// DeclareIn registers a binding in a shared declaration space. The directive
// kind is only a diagnostic label; the compiler does not know extension kinds.
func (req DirectiveReq) DeclareIn(space, kind, name string) error {
	return req.DeclareInAt(space, kind, name, SourceSpan{}, SourceSpan{}, SourceSpan{})
}

// DeclareAt registers a declaration and its original-source spans. It retains
// Declare's duplicate-checking behavior while making the parser's name and full
// directive range available to editor tooling.
func (req DirectiveReq) DeclareAt(kind, name string, keyword, nameSpan, span SourceSpan) error {
	return req.DeclareInAt(kind, kind, name, keyword, nameSpan, span)
}

// DeclareInAt is DeclareIn with source spans supplied by the directive parser.
func (req DirectiveReq) DeclareInAt(space, kind, name string, keyword, nameSpan, span SourceSpan) error {
	if req.Out == nil {
		return SourceError(Position{File: req.File, Line: req.I + 1}, "declaration output is unavailable")
	}
	scope := req.sourceScope()
	key := space + "\x00" + name
	pos := Position{File: req.File, Line: req.I + 1}
	if previous, exists := scope.declarations[key]; exists {
		return SourceError(pos, "duplicate declaration @%s %q; first declared at %s", kind, name, previous)
	}
	scope.declarations[key] = pos
	if keyword != (SourceSpan{}) || nameSpan != (SourceSpan{}) || span != (SourceSpan{}) {
		indexed := false
		for _, declaration := range scope.index.Declarations {
			if declaration.Kind == kind && declaration.Name == name && declaration.NameSpan == nameSpan {
				indexed = true
				break
			}
		}
		if !indexed {
			scope.index.Declarations = append(scope.index.Declarations, SourceDeclaration{
				Kind: kind, Name: name, KeywordSpan: keyword, NameSpan: nameSpan, Span: span,
			})
		}
	}
	return nil
}

// RecordDeclaration adds a parser-recognized declaration without registering it
// in the duplicate-checking table. This lets tolerant editor parsing retain the
// declaration even when a later block/body error stops preprocessing.
func (req DirectiveReq) RecordDeclaration(declaration SourceDeclaration) {
	if scope := req.sourceScope(); scope != nil {
		scope.index.Declarations = append(scope.index.Declarations, declaration)
	}
}

// RecordReference records a reference parsed by a directive handler.
func (req DirectiveReq) RecordReference(reference SourceReference) {
	if scope := req.sourceScope(); scope != nil {
		scope.index.References = append(scope.index.References, reference)
	}
}

// RecordBlock records a directive-owned block and its parser-derived spans.
func (req DirectiveReq) RecordBlock(block SourceBlock) {
	if scope := req.sourceScope(); scope != nil {
		scope.index.Blocks = append(scope.index.Blocks, block)
	}
}

func (req DirectiveReq) sourceScope() *sourceScope {
	if req.Out == nil {
		return nil
	}
	scope, _ := req.Out[sourceScopeKey].(*sourceScope)
	if scope == nil {
		scope = &sourceScope{declarations: make(map[string]Position)}
		req.Out[sourceScopeKey] = scope
	}
	if scope.declarations == nil {
		scope.declarations = make(map[string]Position)
	}
	return scope
}

// Offset returns the byte offset at a 0-based line and byte column in the
// directive request's original source. A request built outside preprocessing
// falls back to offsets computed from Lines.
func (req DirectiveReq) Offset(line, column int) int {
	if line < 0 || line > len(req.Lines) {
		return 0
	}
	if line == len(req.Lines) {
		if line == 0 {
			return 0
		}
		last := line - 1
		start := 0
		if last < len(req.lineOffsets) {
			start = req.lineOffsets[last]
		} else {
			for i := range last {
				start += len(req.Lines[i]) + 1
			}
		}
		return start + len(req.Lines[last])
	}
	start := 0
	if line < len(req.lineOffsets) {
		start = req.lineOffsets[line]
	} else {
		for i := range line {
			start += len(req.Lines[i]) + 1
		}
	}
	return start + min(max(column, 0), len(req.Lines[line]))
}

// LineSpan returns the span of one source line without its line ending.
func (req DirectiveReq) LineSpan(line int) SourceSpan {
	return SourceSpan{Start: req.Offset(line, 0), End: req.Offset(line, len(req.Lines[line]))}
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

func importSourceAt(ctx context.Context, dt *DirectiveTable, loader SourceLoader, stack *includeStack, req DirectiveReq, target, namespace string, span, targetSpan, namespaceSpan SourceSpan, partial bool) error {
	pos := Position{File: req.File, Line: req.I + 1}
	if !validSourceNamespace(namespace) {
		return SourceError(pos, "@include: invalid namespace %q; use AS with an identifier", namespace)
	}
	if err := req.DeclareInAt("namespace", "include", namespace, SourceSpan{}, namespaceSpan, span); err != nil {
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
	childClean, childOut, childErr := preprocess(ctx, dt, string(data), resolvedPath, stack, nil, partial)
	if childOut != nil {
		scope := req.sourceScope()
		scope.imports = append(scope.imports, SourceImport{
			Namespace: namespace, Position: pos, Span: span, TargetSpan: targetSpan, NamespaceSpan: namespaceSpan,
			Source: &PreparedSource{source: string(data), name: resolvedPath, clean: childClean, meta: childOut},
		})
	}
	if childErr != nil {
		return childErr
	}
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
