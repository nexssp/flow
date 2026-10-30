package compiler

import (
	"context"
	"sort"
)

// SourceSpan is a half-open byte range in the original source file.
type SourceSpan struct {
	Start int
	End   int
}

// SourceDeclaration describes a named declaration parsed by a Flow directive.
type SourceDeclaration struct {
	Kind        string
	Name        string
	KeywordSpan SourceSpan
	NameSpan    SourceSpan
	Span        SourceSpan
}

// SourceReference describes a parsed or lexical reference in Flow source.
// Modifier is set for schema modifier references; Value preserves malformed
// values so editor diagnostics can point at the same text the compiler parsed.
type SourceReference struct {
	Kind         string
	Name         string
	Value        string
	Modifier     string
	Span         SourceSpan
	NameSpan     SourceSpan
	ModifierSpan SourceSpan
	HasName      bool
	Valid        bool
}

// SourceBlock describes a directive-owned block in original source coordinates.
type SourceBlock struct {
	Kind       string
	Span       SourceSpan
	HeaderSpan SourceSpan
	BodySpan   SourceSpan
}

// SourceToken is a token emitted by Flow's lexer, with its original byte span.
type SourceToken struct {
	Type TokenType
	Text string
	Span SourceSpan
	// InsideProjection marks tokens within a plain projection primary.
	InsideProjection bool
}

// SourceIndex is the single Flow-owned, source-coordinate view used by editor
// tooling. Directive handlers contribute declarations, references, blocks and
// import edges; lexical references and tokens come from the compiler lexer.
type SourceIndex struct {
	Declarations []SourceDeclaration
	References   []SourceReference
	Blocks       []SourceBlock
	Imports      []SourceImport
	Tokens       []SourceToken
}

// BuildSourceIndex combines records produced by compiler/directive parsing with
// Flow lexer tokens from the original source. The metadata remains the authority
// for directive-owned grammar; this function does not rediscover declarations.
func BuildSourceIndex(source string, meta map[string]any) SourceIndex {
	index := SourceIndex{Imports: SourceImports(meta)}
	if scope, _ := meta[sourceScopeKey].(*sourceScope); scope != nil {
		index.Declarations = append(index.Declarations, scope.index.Declarations...)
		index.References = append(index.References, scope.index.References...)
		index.Blocks = append(index.Blocks, scope.index.Blocks...)
	}
	index.Tokens = sourceTokens(source, 0)
	sort.SliceStable(index.Tokens, func(i, j int) bool {
		return index.Tokens[i].Span.Start < index.Tokens[j].Span.Start
	})
	for _, token := range index.Tokens {
		if token.Type != TokIdent || token.InsideProjection {
			continue
		}
		index.References = append(index.References, SourceReference{
			Kind: "identifier", Name: token.Text, Span: token.Span,
			NameSpan: token.Span, HasName: true, Valid: true,
		})
	}
	index.References = append(index.References, schemaModifierReferences(index.Tokens)...)
	sort.SliceStable(index.Declarations, func(i, j int) bool {
		return index.Declarations[i].NameSpan.Start < index.Declarations[j].NameSpan.Start
	})
	sort.SliceStable(index.References, func(i, j int) bool {
		return index.References[i].Span.Start < index.References[j].Span.Start
	})
	sort.SliceStable(index.Blocks, func(i, j int) bool {
		return index.Blocks[i].Span.Start < index.Blocks[j].Span.Start
	})
	return index
}

// SourceIndex returns the source index of a prepared source without repeating
// preprocessing or directive parsing.
func (p *PreparedSource) SourceIndex() SourceIndex {
	if p == nil {
		return SourceIndex{}
	}
	return BuildSourceIndex(p.source, p.meta)
}

// SourceText returns the original source captured during preparation.
func (p *PreparedSource) SourceText() string {
	if p == nil {
		return ""
	}
	return p.source
}

// IndexSource runs the registered Flow directives in editor mode, retaining
// partial directive records on syntax errors, then combines them with lexer
// tokens from the original source.
func IndexSource(ctx context.Context, dt *DirectiveTable, source, name string, opts ...CompileOption) (SourceIndex, error) {
	index, _, err := IndexSourceWithMetadata(ctx, dt, source, name, opts...)
	return index, err
}

// IndexSourceWithMetadata is IndexSource and also returns partial metadata for
// callers that need directive data such as parsed schema fields after an editor
// document contains a recoverable syntax error.
func IndexSourceWithMetadata(ctx context.Context, dt *DirectiveTable, source, name string, opts ...CompileOption) (SourceIndex, map[string]any, error) {
	cfg := applyCompileOptions(opts)
	ctx = applyConfigToCtx(ctx, cfg)
	_, meta, err := preprocessWithMetaMode(ctx, dt, source, name, nil, true)
	return BuildSourceIndex(source, meta), meta, err
}

func sourceTokens(source string, base int) []SourceToken {
	lexer := NewLexer(source)
	var tokens []SourceToken
	var projectionBraces []bool
	projectionDepth := 0
	previousType := TokEOF
	for token := lexer.Next(); token.Type != TokEOF; token = lexer.Next() {
		span := SourceSpan{Start: base + token.Offset, End: base + token.EndOffset}
		tokens = append(tokens, SourceToken{
			Type: token.Type, Text: token.Lit, Span: span,
			InsideProjection: projectionDepth > 0,
		})
		switch token.Type {
		case TokLBrace:
			// Flow block primaries open after an identifier (race { ... }) or a
			// closing condition (match (...) { ... }); other braces are projections.
			projection := projectionDepth > 0 || previousType != TokIdent && previousType != TokRParen
			projectionBraces = append(projectionBraces, projection)
			if projection {
				projectionDepth++
			}
		case TokRBrace:
			if last := len(projectionBraces) - 1; last >= 0 {
				if projectionBraces[last] {
					projectionDepth--
				}
				projectionBraces = projectionBraces[:last]
			}
		case TokEOF, TokIdent, TokString, TokNumber, TokArrow, TokPipe,
			TokAmpersand, TokOrOr, TokLParen, TokRParen, TokColon, TokEquals,
			TokComma, TokQuestion, TokHash, TokTilde, TokAtBrace, TokAtPrompt:
			// These tokens do not affect projection-depth tracking.
		}
		if token.Type == TokAtBrace && token.Lit != "" {
			tokens = append(tokens, sourceTokens(token.Lit, span.Start+2)...)
		}
		previousType = token.Type
	}
	return tokens
}

func schemaModifierReferences(tokens []SourceToken) []SourceReference {
	var references []SourceReference
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].Type != TokColon || tokens[i+1].Type != TokIdent {
			continue
		}
		modifier := tokens[i+1].Text
		if modifier != "schema" && modifier != "request-schema" && modifier != "response-schema" {
			continue
		}
		if i+2 >= len(tokens) || tokens[i+2].Type != TokEquals {
			continue
		}
		modifierSpan := SourceSpan{Start: tokens[i].Span.Start, End: tokens[i+1].Span.End}
		ref := SourceReference{
			Kind: "schema", Modifier: modifier, ModifierSpan: modifierSpan,
			Span: modifierSpan, NameSpan: SourceSpan{Start: tokens[i+2].Span.End, End: tokens[i+2].Span.End},
		}
		valueIndex := i + 3
		valueStart := ref.NameSpan.Start
		if valueIndex < len(tokens) && tokens[valueIndex].Type == TokIdent && tokens[valueIndex].Text == "!" {
			valueStart = tokens[valueIndex].Span.End
			valueIndex++
		}
		if valueIndex < len(tokens) && tokens[valueIndex].Span.Start == valueStart {
			value := tokens[valueIndex]
			if value.Type == TokIdent || value.Type == TokNumber || value.Type == TokString {
				ref.Value = value.Text
				ref.NameSpan = value.Span
				ref.Span.End = value.Span.End
				ref.HasName = value.Type == TokIdent && isSourceIdentifier(value.Text)
				if ref.HasName {
					ref.Name = value.Text
					ref.Valid = true
				}
			}
		}
		references = append(references, ref)
	}
	return references
}

func isSourceIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		c := value[i]
		if i == 0 {
			if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
				return false
			}
			continue
		}
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
