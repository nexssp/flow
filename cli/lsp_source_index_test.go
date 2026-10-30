package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestLSPSourceIndexUsesDirectiveParserSpans(t *testing.T) {
	source := "@schema Base { ID string }\n" +
		"@schema Request {\n  Base\n}\n" +
		"@pipeline submit\n  :request-schema=!Request\n  runtime.noop\n@end\n"
	index := lspSourceIndexFromText(source)

	declarations := make(map[string]compiler.SourceDeclaration)
	for _, declaration := range index.Declarations {
		declarations[declaration.Kind+":"+declaration.Name] = declaration
	}
	for key, expected := range map[string]string{
		"schema:Base":              "Base",
		"schema:Request":           "Request",
		"pipeline:pipeline.submit": "submit",
	} {
		declaration, exists := declarations[key]
		if !exists {
			t.Fatalf("missing indexed declaration %q: %#v", key, index.Declarations)
		}
		if got := source[declaration.NameSpan.Start:declaration.NameSpan.End]; got != expected {
			t.Errorf("%s name span selects %q, want %q", key, got, expected)
		}
		if declaration.KeywordSpan.End <= declaration.KeywordSpan.Start || declaration.Span.End <= declaration.Span.Start {
			t.Errorf("%s has incomplete source spans: %#v", key, declaration)
		}
	}

	foundEmbed, foundModifier := false, false
	for _, reference := range index.References {
		switch reference.Kind {
		case "schema-embed":
			foundEmbed = true
			if got := source[reference.NameSpan.Start:reference.NameSpan.End]; got != "Base" {
				t.Errorf("embed reference span selects %q, want Base", got)
			}
		case "schema":
			if reference.Modifier != "request-schema" {
				continue
			}
			foundModifier = true
			if got := source[reference.NameSpan.Start:reference.NameSpan.End]; got != "Request" {
				t.Errorf("strict schema reference span selects %q, want Request", got)
			}
		}
	}
	if !foundEmbed || !foundModifier {
		t.Fatalf("SourceIndex references missing embed or modifier spans: %#v", index.References)
	}

	foundSchemaBlock, foundPipelineBlock := false, false
	for _, block := range index.Blocks {
		if block.Span.End <= block.Span.Start || block.BodySpan.End < block.BodySpan.Start {
			t.Errorf("invalid indexed block spans: %#v", block)
		}
		if block.Kind == "schema" && source[block.Span.Start:block.Span.End][0] == '@' {
			foundSchemaBlock = true
		}
		if block.Kind == "pipeline" {
			foundPipelineBlock = true
		}
	}
	if !foundSchemaBlock || !foundPipelineBlock {
		t.Fatalf("SourceIndex blocks missing schema or pipeline: %#v", index.Blocks)
	}
}

func TestLSPSourceIndexPreservesNamespaceImportSpans(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "main.nflow")
	childPath := filepath.Join(dir, "shared.nflow")
	if err := os.WriteFile(childPath, []byte("@schema Input { ID string }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "@include \"shared.nflow\" AS shared\n"
	index, _, err := compiler.IndexSourceWithMetadata(context.Background(), lspIndexDirectives(), source, rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Imports) != 1 {
		t.Fatalf("indexed imports = %#v, want one namespace-aware import", index.Imports)
	}
	imported := index.Imports[0]
	if got := source[imported.TargetSpan.Start:imported.TargetSpan.End]; got != "shared.nflow" {
		t.Errorf("import target span selects %q, want shared.nflow", got)
	}
	if got := source[imported.NamespaceSpan.Start:imported.NamespaceSpan.End]; got != "shared" {
		t.Errorf("import namespace span selects %q, want shared", got)
	}
	if !strings.Contains(source[imported.Span.Start:imported.Span.End], "@include") {
		t.Errorf("import span does not cover directive: %#v", imported.Span)
	}
	if imported.Source == nil || imported.Source.SourceText() != "@schema Input { ID string }\n" {
		t.Errorf("imported source was not preserved: %#v", imported.Source)
	}
}
