package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLanguageServerPipelineActionHoverAndDefinition(t *testing.T) {
	uri := "file:///tmp/pipeline-actions.nflow"
	source := "# @pipeline phantom\n" +
		"@pipeline greet\n" +
		"  runtime.noop\n" +
		"@end\n" +
		"@pipeline caller\n" +
		"  {} -> pipeline.greet\n" +
		"@end\n" +
		"@pipeline text\n" +
		"  { value: \"pipeline.greet\" }\n" +
		"@end\n"
	server := newFlowLanguageServer(context.Background(), nil)
	server.initialized = true
	server.documents[uri] = source
	server.documentVersion[uri] = 1
	var output bytes.Buffer
	server.out = &output

	declarationOffset := strings.Index(source, "greet")
	declarationHover := callSchemaLSPRequest(t, server, &output, "textDocument/hover", uri, source, declarationOffset, nil)
	var hoverResult struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(declarationHover.Result, &hoverResult); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hoverResult.Contents.Value, "pipeline.greet") || !strings.Contains(hoverResult.Contents.Value, "Flow pipeline action") {
		t.Fatalf("pipeline declaration hover = %q, want action name and pipeline details", hoverResult.Contents.Value)
	}
	if got, want := hoverResult.Range.Start, lspPositionForByteOffset(source, declarationOffset); got != want {
		t.Fatalf("pipeline declaration hover starts at %#v, want %#v", got, want)
	}

	useOffset := strings.Index(source, "pipeline.greet")
	hover := callSchemaLSPRequest(t, server, &output, "textDocument/hover", uri, source, useOffset, nil)
	if err := json.Unmarshal(hover.Result, &hoverResult); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hoverResult.Contents.Value, "pipeline.greet") {
		t.Fatalf("pipeline action hover = %q, want pipeline.greet", hoverResult.Contents.Value)
	}
	if got, want := hoverResult.Range.Start, lspPositionForByteOffset(source, useOffset); got != want {
		t.Fatalf("pipeline action hover starts at %#v, want %#v", got, want)
	}

	definition := callSchemaLSPRequest(t, server, &output, "textDocument/definition", uri, source, useOffset, nil)
	var location struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(definition.Result, &location); err != nil {
		t.Fatal(err)
	}
	if location.URI != uri || location.Range.Start != lspPositionForByteOffset(source, declarationOffset) {
		t.Fatalf("pipeline action definition = %#v, want local greet declaration", location)
	}

	commentOffset := strings.Index(source, "phantom")
	comment := callSchemaLSPRequest(t, server, &output, "textDocument/definition", uri, source, commentOffset, nil)
	if string(comment.Result) != "null" {
		t.Fatalf("comment definition = %s, want null", comment.Result)
	}
	stringOffset := strings.LastIndex(source, "pipeline.greet")
	stringResult := callSchemaLSPRequest(t, server, &output, "textDocument/definition", uri, source, stringOffset, nil)
	if string(stringResult.Result) != "null" {
		t.Fatalf("string definition = %s, want null", stringResult.Result)
	}
	unknownOffset := strings.Index(source, "runtime.noop")
	unknown := callSchemaLSPRequest(t, server, &output, "textDocument/definition", uri, source, unknownOffset, nil)
	if string(unknown.Result) != "null" {
		t.Fatalf("external action definition = %s, want null when no source declaration exists", unknown.Result)
	}
}

func TestLanguageServerPipelineActionDefinitionThroughIncludeNamespace(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.nflow")
	libraryPath := filepath.Join(dir, "library.nflow")
	librarySource := "@pipeline charge\n  runtime.noop\n@end\n"
	if err := os.WriteFile(libraryPath, []byte(librarySource), 0o600); err != nil {
		t.Fatal(err)
	}
	mainSource := "@include \"library.nflow\" AS billing\n" +
		"@pipeline caller\n" +
		"  {} -> billing.pipeline.charge\n" +
		"@end\n"
	mainURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(mainPath)}).String()
	libraryURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(libraryPath)}).String()
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		server.initialized = true
		server.documents[mainURI] = mainSource
		server.documentVersion[mainURI] = 1
		var output bytes.Buffer
		server.out = &output

		useOffset := strings.Index(mainSource, "billing.pipeline.charge")
		definition := callSchemaLSPRequest(t, server, &output, "textDocument/definition", mainURI, mainSource, useOffset, nil)
		var location struct {
			URI   string   `json:"uri"`
			Range lspRange `json:"range"`
		}
		if err := json.Unmarshal(definition.Result, &location); err != nil {
			return err
		}
		declarationOffset := strings.Index(librarySource, "charge")
		if location.URI != libraryURI || location.Range.Start != lspPositionForByteOffset(librarySource, declarationOffset) {
			t.Fatalf("imported pipeline definition = %#v, want charge declaration in %s", location, libraryURI)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLSPPipelineSymbolsUseCanonicalActionNames(t *testing.T) {
	symbols := lspPipelineSymbols("@pipeline greet\n@end\n@pipeline jobs.process\n@end\n")
	if len(symbols) != 2 {
		t.Fatalf("pipeline symbols = %#v, want two declarations", symbols)
	}
	if symbols[0].actionName != "pipeline.greet" || symbols[1].actionName != "jobs.process" {
		t.Fatalf("canonical pipeline action names = %#v, want pipeline.greet and jobs.process", []string{symbols[0].actionName, symbols[1].actionName})
	}
}

func TestLanguageServerPipelineReferencesAndRenameIgnoreCommentsStringsAndProjections(t *testing.T) {
	uri := "file:///tmp/pipeline-reference-rename.nflow"
	source := "# pipeline.greet\n" +
		"@pipeline greet\n  runtime.noop\n@end\n" +
		"@pipeline caller\n" +
		"  { pipeline: pipeline.greet }\n" +
		"  pipeline.greet\n" +
		"  race { pipeline.greet }\n" +
		"  match (true) { true -> pipeline.greet }\n" +
		"  { note: \"pipeline.greet\" }\n" +
		"  // pipeline.greet\n" +
		"@end\n"
	server := newFlowLanguageServer(context.Background(), nil)
	server.initialized = true
	server.documents[uri] = source
	server.documentVersion[uri] = 1
	var output bytes.Buffer
	server.out = &output

	useOffset := strings.Index(source, "\n  pipeline.greet") + len("\n  ")
	projectionOffset := strings.Index(source, "{ pipeline: pipeline.greet }") + len("{ pipeline: ")
	raceOffset := strings.Index(source, "race { pipeline.greet }") + len("race { ")
	matchOffset := strings.Index(source, "match (true) { true -> pipeline.greet }") + len("match (true) { true -> ")
	declarationOffset := strings.Index(source, "@pipeline greet") + len("@pipeline ")
	projectionDefinition := callSchemaLSPRequest(t, server, &output, "textDocument/definition", uri, source, projectionOffset, nil)
	if string(projectionDefinition.Result) != "null" {
		t.Fatalf("projection definition = %s, want null", projectionDefinition.Result)
	}
	projectionHover := callSchemaLSPRequest(t, server, &output, "textDocument/hover", uri, source, projectionOffset, nil)
	if string(projectionHover.Result) != "null" {
		t.Fatalf("projection hover = %s, want null", projectionHover.Result)
	}
	response := callSchemaLSPRequest(t, server, &output, "textDocument/references", uri, source, useOffset, map[string]any{"includeDeclaration": true})
	var locations []struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(response.Result, &locations); err != nil {
		t.Fatal(err)
	}
	if len(locations) != 4 {
		t.Fatalf("pipeline.greet references = %#v, want declaration, top-level use, race-block use, and match-block use (not projection/string/comment text)", locations)
	}
	if locations[0].URI != uri || locations[0].Range.Start != lspPositionForByteOffset(source, declarationOffset) ||
		locations[1].URI != uri || locations[1].Range.Start != lspPositionForByteOffset(source, useOffset) ||
		locations[2].URI != uri || locations[2].Range.Start != lspPositionForByteOffset(source, raceOffset) ||
		locations[3].URI != uri || locations[3].Range.Start != lspPositionForByteOffset(source, matchOffset) {
		t.Fatalf("pipeline reference locations = %#v, want declaration and three executable references", locations)
	}

	response = callSchemaLSPRenameRequest(t, server, &output, uri, source, useOffset, "welcome")
	var edit struct {
		Changes map[string][]struct {
			Range   lspRange `json:"range"`
			NewText string   `json:"newText"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(response.Result, &edit); err != nil {
		t.Fatal(err)
	}
	if len(edit.Changes[uri]) != 4 {
		t.Fatalf("pipeline rename edits = %#v, want declaration and three executable references only", edit.Changes)
	}
	editStarts := make(map[lspPosition]bool, len(edit.Changes[uri]))
	for _, textEdit := range edit.Changes[uri] {
		if textEdit.NewText != "welcome" && textEdit.NewText != "pipeline.welcome" {
			t.Errorf("pipeline rename replacement = %q, want declaration or action name", textEdit.NewText)
		}
		editStarts[textEdit.Range.Start] = true
	}
	for _, offset := range []int{declarationOffset, useOffset, raceOffset, matchOffset} {
		if !editStarts[lspPositionForByteOffset(source, offset)] {
			t.Errorf("pipeline rename edits omit expected occurrence at byte %d", offset)
		}
	}
	if editStarts[lspPositionForByteOffset(source, projectionOffset)] {
		t.Errorf("pipeline rename edits include projection member at byte %d", projectionOffset)
	}
}

func TestLanguageServerPipelineReferencesAndRenameThroughIncludeNamespace(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.nflow")
	libraryPath := filepath.Join(dir, "library.nflow")
	mainSource := "@include \"library.nflow\" AS billing\n" +
		"@pipeline caller\n  billing.pipeline.charge\n@end\n"
	librarySource := "@pipeline charge\n  runtime.noop\n@end\n"
	if err := os.WriteFile(libraryPath, []byte(librarySource), 0o600); err != nil {
		t.Fatal(err)
	}
	mainURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(mainPath)}).String()
	libraryURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(libraryPath)}).String()
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		server.initialized = true
		server.documents[mainURI] = mainSource
		server.documentVersion[mainURI] = 1
		var output bytes.Buffer
		server.out = &output
		useOffset := strings.Index(mainSource, "billing.pipeline.charge")

		response := callSchemaLSPRequest(t, server, &output, "textDocument/references", mainURI, mainSource, useOffset, map[string]any{"includeDeclaration": true})
		var locations []struct {
			URI   string   `json:"uri"`
			Range lspRange `json:"range"`
		}
		if err := json.Unmarshal(response.Result, &locations); err != nil {
			return err
		}
		if len(locations) != 2 {
			t.Fatalf("namespaced pipeline references = %#v, want included declaration and caller use", locations)
		}
		foundDeclaration, foundUse := false, false
		for _, location := range locations {
			foundDeclaration = foundDeclaration || location.URI == libraryURI && location.Range.Start == lspPositionForByteOffset(librarySource, strings.Index(librarySource, "charge"))
			foundUse = foundUse || location.URI == mainURI && location.Range.Start == lspPositionForByteOffset(mainSource, useOffset)
		}
		if !foundDeclaration || !foundUse {
			t.Fatalf("namespaced pipeline reference locations = %#v, want declaration in library and use in main", locations)
		}

		response = callSchemaLSPRenameRequest(t, server, &output, mainURI, mainSource, useOffset, "settle")
		var edit struct {
			Changes map[string][]struct {
				NewText string `json:"newText"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(response.Result, &edit); err != nil {
			return err
		}
		if len(edit.Changes[libraryURI]) != 1 || edit.Changes[libraryURI][0].NewText != "settle" ||
			len(edit.Changes[mainURI]) != 1 || edit.Changes[mainURI][0].NewText != "billing.pipeline.settle" {
			t.Fatalf("namespaced pipeline rename edits = %#v, want library declaration and qualified caller use", edit.Changes)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLanguageServerPipelineRenameRejectsCollisionsAndAmbiguousReferences(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		old    string
		new    string
	}{
		{
			name:   "would bind unresolved reference",
			source: "@pipeline old\n@end\n@pipeline caller\n  pipeline.old\n  pipeline.fresh\n@end\n",
			old:    "old", new: "fresh",
		},
		{
			name:   "collides with another declaration",
			source: "@pipeline old\n@end\n@pipeline fresh\n@end\n@pipeline caller\n  pipeline.old\n@end\n",
			old:    "old", new: "fresh",
		},
		{
			name:   "existing reference is ambiguous",
			source: "@pipeline old\n@end\n@pipeline old\n@end\n@pipeline caller\n  pipeline.old\n@end\n",
			old:    "old", new: "fresh",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := "file:///tmp/pipeline-rename-unsafe.nflow"
			server := newFlowLanguageServer(context.Background(), nil)
			server.initialized = true
			server.documents[uri] = tc.source
			server.documentVersion[uri] = 1
			var output bytes.Buffer
			server.out = &output
			declarationOffset := strings.Index(tc.source, "@pipeline "+tc.old) + len("@pipeline ")
			response := callSchemaLSPRenameRequest(t, server, &output, uri, tc.source, declarationOffset, tc.new)
			if string(response.Result) != "null" {
				t.Fatalf("unsafe pipeline rename result = %s, want null/no edit", response.Result)
			}
		})
	}
}
