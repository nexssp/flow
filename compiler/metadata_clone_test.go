package compiler

import "testing"

func TestPreparedSourceMetadataClonesSourceLineOrigins(t *testing.T) {
	prepared := &PreparedSource{meta: map[string]any{
		sourceLinesKey: []SourceLine{{Origins: []Position{{File: "source.nflow", Line: 1}}}},
	}}

	first := prepared.Metadata()
	lines, ok := first[sourceLinesKey].([]SourceLine)
	if !ok {
		t.Fatalf("source-line metadata type = %T, want []SourceLine", first[sourceLinesKey])
	}
	lines[0].Origins[0].File = "changed.nflow"

	second := prepared.Metadata()
	secondLines, ok := second[sourceLinesKey].([]SourceLine)
	if !ok {
		t.Fatalf("source-line metadata type on second call = %T, want []SourceLine", second[sourceLinesKey])
	}
	if got := secondLines[0].Origins[0].File; got != "source.nflow" {
		t.Fatalf("source-line origin after snapshot mutation = %q, want source.nflow", got)
	}
}

func TestPreparedSourceMetadataClonesSourceScopeContainers(t *testing.T) {
	original := &sourceScope{
		declarations: map[string]Position{"schema\x00original": {File: "source.nflow", Line: 1}},
		imports:      []SourceImport{{Namespace: "original"}},
		index: SourceIndex{
			Declarations: []SourceDeclaration{{Kind: "schema", Name: "original"}},
			References:   []SourceReference{{Kind: "identifier", Name: "original"}},
			Blocks:       []SourceBlock{{Kind: "schema"}},
			Imports:      []SourceImport{{Namespace: "original-index"}},
			Tokens:       []SourceToken{{Text: "original-token"}},
		},
	}
	prepared := &PreparedSource{meta: map[string]any{sourceScopeKey: original}}

	first := prepared.Metadata()
	copied, ok := first[sourceScopeKey].(*sourceScope)
	if !ok {
		t.Fatalf("source-scope metadata type = %T, want *sourceScope", first[sourceScopeKey])
	}
	if copied == original {
		t.Fatal("source-scope pointer was shared with the prepared source")
	}
	copied.declarations["schema\x00original"] = Position{File: "changed.nflow", Line: 2}
	copied.imports[0].Namespace = "changed"
	copied.index.Declarations[0].Name = "changed"
	copied.index.References[0].Name = "changed"
	copied.index.Blocks[0].Kind = "changed"
	copied.index.Imports[0].Namespace = "changed"
	copied.index.Tokens[0].Text = "changed"

	second := prepared.Metadata()
	secondScope, ok := second[sourceScopeKey].(*sourceScope)
	if !ok {
		t.Fatalf("source-scope metadata type on second call = %T, want *sourceScope", second[sourceScopeKey])
	}
	if got := secondScope.declarations["schema\x00original"].File; got != "source.nflow" {
		t.Fatalf("source-scope declaration after snapshot mutation = %q, want source.nflow", got)
	}
	if got := secondScope.imports[0].Namespace; got != "original" {
		t.Fatalf("source-scope import after snapshot mutation = %q, want original", got)
	}
	if got := secondScope.index.Declarations[0].Name; got != "original" {
		t.Fatalf("source-index declaration after snapshot mutation = %q, want original", got)
	}
	if got := secondScope.index.References[0].Name; got != "original" {
		t.Fatalf("source-index reference after snapshot mutation = %q, want original", got)
	}
	if got := secondScope.index.Blocks[0].Kind; got != "schema" {
		t.Fatalf("source-index block after snapshot mutation = %q, want schema", got)
	}
	if got := secondScope.index.Imports[0].Namespace; got != "original-index" {
		t.Fatalf("source-index import after snapshot mutation = %q, want original-index", got)
	}
	if got := secondScope.index.Tokens[0].Text; got != "original-token" {
		t.Fatalf("source-index token after snapshot mutation = %q, want original-token", got)
	}
}
