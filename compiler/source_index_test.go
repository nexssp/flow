package compiler

import (
	"context"
	"errors"
	"testing"
)

func TestPreparedSourceFromPreprocessedDoesNotRunDirectivesAgain(t *testing.T) {
	calls := 0
	directive := Directive{
		Name: "mark",
		Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
			calls++
			req.RecordBlock(SourceBlock{Kind: "mark", Span: req.LineSpan(req.I)})
			return DirectiveRes{Next: req.I + 1}, nil
		},
	}
	directives := NewDirectiveTable(directive)
	source := "@mark\nruntime.noop\n"
	clean, metadata, err := PreprocessWithOptions(context.Background(), directives, source, "test.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("directive calls after preprocess = %d, want one", calls)
	}

	prepared := PreparedSourceFromPreprocessed(source, "test.nflow", clean, metadata)
	if prepared.SourceText() != source {
		t.Fatalf("SourceText() = %q, want original source", prepared.SourceText())
	}
	if prepared.CleanSource() != clean {
		t.Fatalf("CleanSource() = %q, want %q", prepared.CleanSource(), clean)
	}
	if got := prepared.SourceIndex().Blocks; len(got) != 1 || got[0].Kind != "mark" {
		t.Fatalf("prepared source index blocks = %#v, want the parser-recorded block", got)
	}
	if calls != 1 {
		t.Fatalf("directive calls after constructing/indexing prepared source = %d, want one", calls)
	}
}

func TestIndexSourceContinuesAfterRecoverableDirectiveError(t *testing.T) {
	wantErr := errors.New("invalid directive")
	directives := NewDirectiveTable(
		Directive{
			Name: "invalid",
			Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
				return DirectiveRes{Next: req.I + 1}, wantErr
			},
		},
		Directive{
			Name: "later",
			Handler: func(_ context.Context, req DirectiveReq) (DirectiveRes, error) {
				req.RecordDeclaration(SourceDeclaration{
					Kind: "schema", Name: "later", NameSpan: SourceSpan{Start: req.Offset(req.I, 1), End: req.Offset(req.I, 6)},
				})
				return DirectiveRes{Next: req.I + 1}, nil
			},
		},
	)
	index, _, err := IndexSourceWithMetadata(context.Background(), directives, "@invalid\n@later\n", "test.nflow")
	if !errors.Is(err, wantErr) {
		t.Fatalf("IndexSourceWithMetadata error = %v, want invalid directive", err)
	}
	if len(index.Declarations) != 1 || index.Declarations[0].Name != "later" {
		t.Fatalf("index after recoverable error = %#v, want later declaration", index.Declarations)
	}
}

func TestBuildSourceIndexExcludesProjectionIdentifiersFromReferences(t *testing.T) {
	source := "{ pipeline: pipeline.greet, nested: { pipeline: pipeline.greet } }\n" +
		"pipeline.greet\n" +
		"race { pipeline.greet }\n" +
		"match (true) { true -> pipeline.greet }\n"
	index := BuildSourceIndex(source, nil)

	var references []SourceReference
	var tokens []SourceToken
	for _, reference := range index.References {
		if reference.Kind == "identifier" && reference.Name == "pipeline.greet" {
			references = append(references, reference)
		}
	}
	for _, token := range index.Tokens {
		if token.Type == TokIdent && token.Text == "pipeline.greet" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) != 5 {
		t.Fatalf("pipeline.greet tokens = %#v, want all five lexical occurrences", tokens)
	}
	if len(references) != 3 {
		t.Fatalf("pipeline.greet references = %#v, want only the top-level, race-block, and match-block occurrences", references)
	}
	if references[0].NameSpan.Start != tokens[2].Span.Start || references[1].NameSpan.Start != tokens[3].Span.Start ||
		references[2].NameSpan.Start != tokens[4].Span.Start {
		t.Fatalf("pipeline.greet references = %#v, want occurrences at %#v, %#v, and %#v", references, tokens[2].Span, tokens[3].Span, tokens[4].Span)
	}
}
