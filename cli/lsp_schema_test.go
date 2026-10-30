package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLanguageServerSchemaHoverDefinitionAndReferences(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	uri := "file:///tmp/schema-intelligence.nflow"
	source := "@schema Shared {\n" +
		"  ID string `json:\"id\" validate:\"required\" usage:\"Stable identifier\"`\n" +
		"}\n" +
		"@schema Request {\n" +
		"  Shared\n" +
		"  Name string `json:\"name\" default:\"guest\"`\n" +
		"}\n" +
		"@pipeline submit\n" +
		"  :request-schema=!Request\n" +
		"  runtime.noop\n" +
		"@end\n"

	var output bytes.Buffer
	server := newFlowLanguageServer(context.Background(), inv)
	server.out = &output
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	initialize := readLSPTestMessages(t, &output)
	if len(initialize) != 1 {
		t.Fatalf("initialize responses = %d, want 1", len(initialize))
	}
	var initialized struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(initialize[0].Result, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.Capabilities["hoverProvider"] != true || initialized.Capabilities["definitionProvider"] != true || initialized.Capabilities["referencesProvider"] != true || initialized.Capabilities["renameProvider"] != true || initialized.Capabilities["codeActionProvider"] == nil {
		t.Fatalf("schema capabilities = %#v, want hover, definition, references, rename, and code-action providers", initialized.Capabilities)
	}
	server.documents[uri] = source
	server.documentVersion[uri] = 1

	requestOffset := strings.Index(source, "!Request") + 1
	hover := callSchemaLSPRequest(t, server, &output, "textDocument/hover", uri, source, requestOffset, map[string]any{})
	if len(hover.Result) == 0 || string(hover.Result) == "null" {
		t.Fatalf("schema hover result = %s, want schema details", hover.Result)
	}
	var hoverResult struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(hover.Result, &hoverResult); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Request", "Fields (2)", "Name", `default "guest"`, "ID", "Stable identifier"} {
		if !strings.Contains(hoverResult.Contents.Value, expected) {
			t.Errorf("schema hover %q does not contain %q", hoverResult.Contents.Value, expected)
		}
	}
	if got, want := hoverResult.Range.Start, lspPositionForByteOffset(source, requestOffset); got != want {
		t.Errorf("hover range starts at %#v, want %#v", got, want)
	}

	definition := callSchemaLSPRequest(t, server, &output, "textDocument/definition", uri, source, requestOffset, map[string]any{})
	var location struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(definition.Result, &location); err != nil {
		t.Fatal(err)
	}
	declarationOffset := strings.Index(source, "Request")
	if location.URI != uri || location.Range.Start != lspPositionForByteOffset(source, declarationOffset) {
		t.Fatalf("schema definition = %#v, want %s at the declaration", location, uri)
	}

	references := callSchemaLSPRequest(t, server, &output, "textDocument/references", uri, source, requestOffset, map[string]any{"includeDeclaration": true})
	var locations []struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(references.Result, &locations); err != nil {
		t.Fatal(err)
	}
	if len(locations) != 2 {
		t.Fatalf("Request references = %#v, want declaration and modifier use", locations)
	}
	if locations[0].Range.Start != lspPositionForByteOffset(source, declarationOffset) || locations[1].Range.Start != lspPositionForByteOffset(source, requestOffset) {
		t.Fatalf("Request reference ranges = %#v, want declaration and modifier positions", locations)
	}

	sharedOffset := strings.Index(source, "Shared\n")
	sharedReferences := callSchemaLSPRequest(t, server, &output, "textDocument/references", uri, source, sharedOffset, map[string]any{"includeDeclaration": false})
	if err := json.Unmarshal(sharedReferences.Result, &locations); err != nil {
		t.Fatal(err)
	}
	if len(locations) != 1 || locations[0].Range.Start != lspPositionForByteOffset(source, sharedOffset) {
		t.Fatalf("embedded schema references = %#v, want only the embed when declaration is excluded", locations)
	}

	noSymbol := strings.Index(source, "runtime.noop")
	unknownHover := callSchemaLSPRequest(t, server, &output, "textDocument/hover", uri, source, noSymbol, map[string]any{})
	if string(unknownHover.Result) != "null" {
		t.Fatalf("non-schema hover = %s, want null rather than fabricated metadata", unknownHover.Result)
	}

	updatedSource := strings.ReplaceAll(source, "Request", "Response")
	changeParams, err := json.Marshal(map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": updatedSource}},
	})
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := server.handle(lspMessage{JSONRPC: "2.0", Method: "textDocument/didChange", Params: changeParams}); err != nil {
		t.Fatal(err)
	}
	if entry, cached := server.schemaCache[uri]; !cached || entry.source != updatedSource || entry.version != 2 {
		t.Fatalf("document edit did not replace cached source index: cached=%v version=%d", cached, entry.version)
	} else if _, stale := entry.index.declarations["Request"]; stale {
		t.Fatal("document edit retained the stale Request schema declaration")
	}
	responseOffset := strings.Index(updatedSource, "!Response") + 1
	updatedDefinition := callSchemaLSPRequest(t, server, &output, "textDocument/definition", uri, updatedSource, responseOffset, map[string]any{})
	if err := json.Unmarshal(updatedDefinition.Result, &location); err != nil {
		t.Fatal(err)
	}
	responseDeclaration := strings.Index(updatedSource, "Response")
	if location.Range.Start != lspPositionForByteOffset(updatedSource, responseDeclaration) {
		t.Fatalf("definition after document change = %#v, want edited Response declaration", location)
	}
}

func TestLSPSchemaIndexIgnoresCommentsAndStrings(t *testing.T) {
	source := "# @schema Fake {\n" +
		"@schema Real {\n" +
		"  Value string\n" +
		"}\n" +
		"@const.load \"settings :schema=Fake\" as settings :schema=Real # :schema=Fake\n" +
		"// :request-schema=Fake\n"
	index := indexLSPFlowSchemas(source)
	if _, exists := index.declarations["Fake"]; exists {
		t.Fatal("schema declaration inside a comment was indexed")
	}
	if _, exists := index.declarations["Real"]; !exists {
		t.Fatal("real schema declaration was not indexed")
	}
	fakeOccurrences := 0
	for _, occurrence := range index.occurrences {
		if occurrence.name == "Fake" {
			fakeOccurrences++
		}
	}
	if fakeOccurrences != 0 {
		t.Fatalf("schema-looking string/comment occurrences = %d, want 0", fakeOccurrences)
	}
	if got := countLSPTestOccurrences(index.occurrences, "Real"); got != 2 {
		t.Fatalf("Real schema occurrences = %d, want declaration and modifier", got)
	}
}

func TestLSPPositionConversionUsesUTF16AndRejectsOutOfRange(t *testing.T) {
	source := "😀 @schema Item {\r\n  ID string\r\n}"
	nameOffset := strings.Index(source, "Item")
	position := lspPositionForByteOffset(source, nameOffset)
	if position != (lspPosition{Line: 0, Character: 11}) {
		t.Fatalf("schema name position = %#v, want UTF-16 character 11", position)
	}
	if got, ok := lspPositionToByteOffset(source, position); !ok || got != nameOffset {
		t.Fatalf("UTF-16 position converted to byte offset %d, %t; want %d", got, ok, nameOffset)
	}
	if _, ok := lspPositionToByteOffset(source, lspPosition{Line: 99, Character: 0}); ok {
		t.Fatal("out-of-range LSP line unexpectedly converted")
	}
}

func callSchemaLSPRequest(t *testing.T, server *flowLanguageServer, output *bytes.Buffer, method, uri, source string, byteOffset int, requestContext map[string]any) lspMessage {
	t.Helper()
	output.Reset()
	position := lspPositionForByteOffset(source, byteOffset)
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	}
	if requestContext != nil {
		params["context"] = requestContext
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("2"), Method: method, Params: paramsJSON,
	}); err != nil {
		t.Fatal(err)
	}
	messages := readLSPTestMessages(t, output)
	if len(messages) != 1 {
		t.Fatalf("%s responses = %d, want 1", method, len(messages))
	}
	return messages[0]
}

func countLSPTestOccurrences(occurrences []lspSchemaOccurrence, name string) int {
	count := 0
	for _, occurrence := range occurrences {
		if occurrence.name == name {
			count++
		}
	}
	return count
}

func TestLanguageServerSchemaLookupDoesNotCrossOpenDocuments(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mainURI := "file:///tmp/schema-workspace/main.nflow"
	sharedURI := "file:///tmp/schema-workspace/shared.nflow"
	mainSource := "@pipeline submit\n  :request-schema=!Shared\n  runtime.noop\n@end\n"
	sharedSource := "@schema Shared {\n  ID string `json:\"id\"`\n}\n"
	server := newFlowLanguageServer(context.Background(), inv)
	server.initialized = true
	server.documents[mainURI] = mainSource
	server.documentVersion[mainURI] = 1
	server.documents[sharedURI] = sharedSource
	server.documentVersion[sharedURI] = 1
	var output bytes.Buffer
	server.out = &output

	useOffset := strings.Index(mainSource, "!Shared") + 1
	definition := callSchemaLSPRequest(t, server, &output, "textDocument/definition", mainURI, mainSource, useOffset, nil)
	var location struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(definition.Result, &location); err != nil {
		t.Fatal(err)
	}
	if location.URI != "" {
		t.Fatalf("unimported cross-document schema definition = %#v, want no definition", location)
	}

	sharedOffset := strings.Index(sharedSource, "Shared")
	references := callSchemaLSPRequest(t, server, &output, "textDocument/references", sharedURI, sharedSource, sharedOffset, map[string]any{"includeDeclaration": true})
	var locations []struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	}
	if err := json.Unmarshal(references.Result, &locations); err != nil {
		t.Fatal(err)
	}
	if len(locations) != 1 || locations[0].URI != sharedURI {
		t.Fatalf("unimported cross-document schema references = %#v, want only its local declaration", locations)
	}

	hover := callSchemaLSPRequest(t, server, &output, "textDocument/hover", mainURI, mainSource, useOffset, nil)
	var hoverResult struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(hover.Result, &hoverResult); err != nil {
		t.Fatal(err)
	}
	if string(hover.Result) != "null" || hoverResult.Contents.Value != "" {
		t.Fatalf("unimported cross-document schema hover = %q (%s), want null", hoverResult.Contents.Value, hover.Result)
	}
}

func TestLanguageServerSchemaLookupDoesNotLeakFromIncludedSource(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "main.nflow")
	childPath := filepath.Join(dir, "shared.nflow")
	childSource := "@schema Imported {\n  ID string\n}\n"
	if err := os.WriteFile(childPath, []byte(childSource), 0o600); err != nil {
		t.Fatal(err)
	}
	mainSource := "@include \"shared.nflow\" AS shared\n@pipeline submit\n  :request-schema=Imported\n  runtime.noop\n@end\n"
	mainURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(rootPath)}).String()
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		server.initialized = true
		server.documents[mainURI] = mainSource
		server.documentVersion[mainURI] = 1
		var output bytes.Buffer
		server.out = &output
		useOffset := strings.Index(mainSource, "Imported")
		definition := callSchemaLSPRequest(t, server, &output, "textDocument/definition", mainURI, mainSource, useOffset, nil)
		var location struct {
			URI   string   `json:"uri"`
			Range lspRange `json:"range"`
		}
		if err := json.Unmarshal(definition.Result, &location); err != nil {
			return err
		}
		if location.URI != "" {
			t.Fatalf("included-source schema definition = %#v, want no parent-scope binding", location)
		}
		references := callSchemaLSPRequest(t, server, &output, "textDocument/references", mainURI, mainSource, useOffset, map[string]any{"includeDeclaration": true})
		var locations []struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(references.Result, &locations); err != nil {
			return err
		}
		if len(locations) != 0 {
			t.Fatalf("included-source references = %#v, want none in the parent schema scope", locations)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLanguageServerCompletesSchemaModifiersAndRequestFieldsWithinImportScope(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "main.nflow")
	childPath := filepath.Join(dir, "shared.nflow")
	childSource := "@schema Input {\n  Name string `json:\"name\"`\n  Count int\n}\n"
	if err := os.WriteFile(childPath, []byte(childSource), 0o600); err != nil {
		t.Fatal(err)
	}
	mainSource := "@include \"shared.nflow\" AS shared\n" +
		"@schema LocalInput {\n  Name string `json:\"name\"`\n  Count int\n}\n" +
		"@pipeline greet\n" +
		"  :request-schema=LocalInput\n" +
		"  { message: .name }\n" +
		"@end\n"
	mainURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(rootPath)}).String()
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

		type item struct {
			Label      string `json:"label"`
			Detail     string `json:"detail"`
			InsertText string `json:"insertText"`
			TextEdit   struct {
				NewText string   `json:"newText"`
				Range   lspRange `json:"range"`
			} `json:"textEdit"`
		}
		completionItems := func(offset int) []item {
			t.Helper()
			response := callSchemaLSPRequest(t, server, &output, "textDocument/completion", mainURI, mainSource, offset, nil)
			var result struct {
				Items []item `json:"items"`
			}
			if err := json.Unmarshal(response.Result, &result); err != nil {
				t.Fatal(err)
			}
			return result.Items
		}
		findItem := func(items []item, label string) (item, bool) {
			t.Helper()
			for _, candidate := range items {
				if candidate.Label == label {
					return candidate, true
				}
			}
			return item{}, false
		}

		modifierStart := strings.Index(mainSource, "=LocalInput") + 1
		modifierCursor := modifierStart + len("Loc")
		modifierItems := completionItems(modifierCursor)
		modifierItem, found := findItem(modifierItems, "LocalInput")
		if !found {
			t.Fatal("schema modifier completion did not include the local LocalInput schema")
		}
		if _, leaked := findItem(modifierItems, "Input"); leaked {
			t.Fatal("schema modifier completion leaked Input from the included module")
		}
		if modifierItem.InsertText != "LocalInput" || modifierItem.TextEdit.NewText != "LocalInput" {
			t.Fatalf("schema completion item = %#v, want LocalInput insertion", modifierItem)
		}
		if got, want := modifierItem.TextEdit.Range, lspRangeForByteOffsets(mainSource, modifierStart, modifierStart+len("LocalInput")); got != want {
			t.Fatalf("schema completion range = %#v, want %#v", got, want)
		}

		fieldStart := strings.Index(mainSource, ".name") + 1
		fieldCursor := fieldStart + len("na")
		fieldItem, found := findItem(completionItems(fieldCursor), "name")
		if !found {
			t.Fatal("request-schema field completion did not include the JSON field name")
		}
		if !strings.Contains(fieldItem.Detail, "Input field") || fieldItem.InsertText != "name" {
			t.Fatalf("schema field completion item = %#v, want Input field name metadata", fieldItem)
		}
		if got, want := fieldItem.TextEdit.Range, lspRangeForByteOffsets(mainSource, fieldStart, fieldStart+len("name")); got != want {
			t.Fatalf("schema field completion range = %#v, want %#v", got, want)
		}
		if _, found := findItem(completionItems(fieldCursor), "flow-pipe"); !found {
			t.Fatal("schema-aware completion replaced the existing general snippets")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLanguageServerSchemaDiagnosticsAndQuickFixes(t *testing.T) {
	uri := "file:///tmp/schema-quick-fix.nflow"
	source := "@schema Request {\n  ID string\n}\n" +
		"@pipeline submit\n  :request-schema=Requset\n  runtime.noop\n@end\n" +
		"@pipeline legacy\n  :schema=Request\n  runtime.noop\n@end\n" +
		"runtime.noop:schema=Request\n" +
		"@pipeline incomplete\n  :request-schema=!\n  runtime.noop\n@end\n" +
		"@pipeline malformed\n  :request-schema=123\n  runtime.noop\n@end\n"
	server := newFlowLanguageServer(context.Background(), nil)
	server.initialized = true
	server.documents[uri] = source
	server.documentVersion[uri] = 1
	var output bytes.Buffer
	server.out = &output

	issues := server.schemaIssues(uri, source)
	unknownOffset := strings.Index(source, "Requset")
	deprecatedOffset := strings.Index(source, ":schema=Request")
	missingOffset := strings.Index(source, ":request-schema=!") + len(":request-schema=!")
	byCode := make(map[string]lspSchemaIssue)
	for _, issue := range issues {
		byCode[issue.diagnostic.Code] = issue
	}
	unknown, ok := byCode["schema.unknown-name"]
	if !ok {
		t.Fatalf("schema issues = %#v, want unresolved-name diagnostic", issues)
	}
	if unknown.diagnostic.Range != lspRangeForByteOffsets(source, unknownOffset, unknownOffset+len("Requset")) {
		t.Fatalf("unknown schema diagnostic range = %#v", unknown.diagnostic.Range)
	}
	if !strings.Contains(unknown.diagnostic.Message, `did you mean "Request"?`) || unknown.fixText != "Request" {
		t.Fatalf("unknown schema issue = %#v, want a unique Request suggestion", unknown)
	}
	deprecated, ok := byCode["schema.deprecated-modifier"]
	if !ok || deprecated.diagnostic.Severity != 2 || deprecated.fixText != ":request-schema" {
		t.Fatalf("deprecated schema issue = %#v, want warning and migration fix", deprecated)
	}
	if deprecated.diagnostic.Range != lspRangeForByteOffsets(source, deprecatedOffset, deprecatedOffset+len(":schema")) {
		t.Fatalf("deprecated modifier diagnostic range = %#v", deprecated.diagnostic.Range)
	}
	if _, ok := byCode["schema.missing-name"]; !ok {
		t.Fatalf("schema issues = %#v, want diagnostic for empty strict reference", issues)
	}
	if _, ok := byCode["schema.invalid-reference"]; !ok {
		t.Fatalf("schema issues = %#v, want diagnostic for malformed numeric reference", issues)
	}
	for _, issue := range issues {
		if issue.diagnostic.Range.Start == lspPositionForByteOffset(source, strings.Index(source, "runtime.noop:schema=Request")+len("runtime.noop")) && issue.diagnostic.Code == "schema.deprecated-modifier" {
			t.Fatal("valid :schema action modifier was incorrectly marked deprecated")
		}
	}

	unknownActions := requestSchemaCodeActions(t, server, &output, uri, source, unknownOffset, unknownOffset+len("Requset"))
	if len(unknownActions) != 1 || unknownActions[0].Title != "Replace with Request" || unknownActions[0].Edit.Changes[uri][0].NewText != "Request" {
		t.Fatalf("unknown-name quick fixes = %#v, want one Request replacement", unknownActions)
	}
	if got, want := unknownActions[0].Edit.Changes[uri][0].Range, lspRangeForByteOffsets(source, unknownOffset, unknownOffset+len("Requset")); got != want {
		t.Fatalf("unknown-name quick-fix range = %#v, want %#v", got, want)
	}
	deprecatedActions := requestSchemaCodeActions(t, server, &output, uri, source, deprecatedOffset, deprecatedOffset+len(":schema"))
	if len(deprecatedActions) != 1 || deprecatedActions[0].Title != "Replace with :request-schema" || deprecatedActions[0].Edit.Changes[uri][0].NewText != ":request-schema" {
		t.Fatalf("deprecated-modifier quick fixes = %#v, want request-schema migration", deprecatedActions)
	}
	missingActions := requestSchemaCodeActions(t, server, &output, uri, source, missingOffset, missingOffset)
	if len(missingActions) != 0 {
		t.Fatalf("empty schema reference quick fixes = %#v, want none rather than an unsafe guess", missingActions)
	}
	if _, found := lspClosestSchemaName("Abc", []string{"Abd", "Abx"}); found {
		t.Fatal("ambiguous closest schema name produced an unsafe suggestion")
	}

	plainURI := "file:///tmp/schema-no-local-declarations.nflow"
	plainSource := "runtime.noop:schema=ExternalType\n"
	plainServer := newFlowLanguageServer(context.Background(), nil)
	plainServer.documents[plainURI] = plainSource
	if plainIssues := plainServer.schemaIssues(plainURI, plainSource); len(plainIssues) != 0 {
		t.Fatalf("generic :schema tag without local declarations produced false diagnostics: %#v", plainIssues)
	}
}

func TestLanguageServerPublishesSchemaDiagnosticsOnOpen(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	uri := "file:///tmp/schema-diagnostic-publication.nflow"
	source := "@schema Request {\n  ID string\n}\n" +
		"@pipeline submit\n  :request-schema=Requset\n  runtime.noop\n@end\n"
	var output bytes.Buffer
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		server.initialized = true
		server.out = &output
		params, marshalErr := json.Marshal(map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": 1, "text": source},
		})
		if marshalErr != nil {
			return marshalErr
		}
		if handleErr := server.handle(lspMessage{JSONRPC: "2.0", Method: "textDocument/didOpen", Params: params}); handleErr != nil {
			return handleErr
		}
		messages := readLSPTestMessages(t, &output)
		for _, message := range messages {
			if message.Method != "textDocument/publishDiagnostics" {
				continue
			}
			var published struct {
				Diagnostics []lspDiagnostic `json:"diagnostics"`
			}
			if decodeErr := json.Unmarshal(message.Params, &published); decodeErr != nil {
				return decodeErr
			}
			for _, diagnostic := range published.Diagnostics {
				if diagnostic.Code == "schema.unknown-name" && strings.Contains(diagnostic.Message, `did you mean "Request"?`) {
					return nil
				}
			}
			return fmt.Errorf("published diagnostics = %#v, want actionable unresolved-schema diagnostic", published.Diagnostics)
		}
		return errors.New("didOpen did not publish diagnostics")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func requestSchemaCodeActions(t *testing.T, server *flowLanguageServer, output *bytes.Buffer, uri, source string, start, end int) []struct {
	Title string `json:"title"`
	Edit  struct {
		Changes map[string][]struct {
			Range   lspRange `json:"range"`
			NewText string   `json:"newText"`
		} `json:"changes"`
	} `json:"edit"`
} {
	t.Helper()
	output.Reset()
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        lspRangeForByteOffsets(source, start, end),
		"context":      map[string]any{"diagnostics": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.handle(lspMessage{JSONRPC: "2.0", ID: json.RawMessage("3"), Method: "textDocument/codeAction", Params: params}); err != nil {
		t.Fatal(err)
	}
	messages := readLSPTestMessages(t, output)
	if len(messages) != 1 {
		t.Fatalf("code-action responses = %d, want 1", len(messages))
	}
	var actions []struct {
		Title string `json:"title"`
		Edit  struct {
			Changes map[string][]struct {
				Range   lspRange `json:"range"`
				NewText string   `json:"newText"`
			} `json:"changes"`
		} `json:"edit"`
	}
	if err := json.Unmarshal(messages[0].Result, &actions); err != nil {
		t.Fatal(err)
	}
	return actions
}

func TestLanguageServerSchemaRenameStaysWithinSourceScope(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.nflow")
	firstPath := filepath.Join(dir, "first.nflow")
	secondPath := filepath.Join(dir, "second.nflow")
	mainSource := "@schema Shared { Root string }\n" +
		"@include \"first.nflow\" AS first\n" +
		"@pipeline submit\n  :request-schema=Shared\n  runtime.noop:schema=Shared\n@end\n" +
		"# Shared in a comment must stay unchanged\n"
	firstSource := "@include \"second.nflow\" AS nested\n"
	secondSource := "@schema Shared {\n  ID string `json:\"Shared\"`\n}\n"
	peerSource := "runtime.noop:schema=Shared\n"
	if err := os.WriteFile(firstPath, []byte(firstSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(secondSource), 0o600); err != nil {
		t.Fatal(err)
	}
	mainURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(mainPath)}).String()
	firstURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(firstPath)}).String()
	secondURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(secondPath)}).String()
	peerURI := "file:///tmp/schema-rename-peer.nflow"
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		server.initialized = true
		server.documents[mainURI] = mainSource
		server.documentVersion[mainURI] = 1
		server.documents[peerURI] = peerSource
		server.documentVersion[peerURI] = 1
		var output bytes.Buffer
		server.out = &output

		selectedOffset := strings.Index(mainSource, "Shared")
		response := callSchemaLSPRenameRequest(t, server, &output, mainURI, mainSource, selectedOffset, "Renamed")
		if len(response.Error) != 0 {
			return fmt.Errorf("schema rename failed: %s", response.Error)
		}
		var result struct {
			Changes map[string][]struct {
				Range   lspRange `json:"range"`
				NewText string   `json:"newText"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(response.Result, &result); err != nil {
			return err
		}
		wantCounts := map[string]int{mainURI: 3}
		if len(result.Changes) != len(wantCounts) {
			return fmt.Errorf("rename changes cover %d source scopes, want %d: %#v (response %s)", len(result.Changes), len(wantCounts), result.Changes, response.Result)
		}
		for _, unrelatedURI := range []string{firstURI, secondURI, peerURI} {
			if _, changed := result.Changes[unrelatedURI]; changed {
				return fmt.Errorf("rename unexpectedly edited a different source scope %s", unrelatedURI)
			}
		}
		for uri, wantCount := range wantCounts {
			edits := result.Changes[uri]
			if len(edits) != wantCount {
				return fmt.Errorf("rename edits for %s = %d, want %d: %#v", uri, len(edits), wantCount, edits)
			}
			for _, edit := range edits {
				if edit.NewText != "Renamed" {
					return fmt.Errorf("rename edit for %s inserts %q, want Renamed", uri, edit.NewText)
				}
			}
		}
		if got, want := result.Changes[mainURI][0].Range, lspRangeForByteOffsets(mainSource, selectedOffset, selectedOffset+len("Shared")); got != want {
			return fmt.Errorf("selected schema rename range = %#v, want %#v", got, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLanguageServerSchemaRenameRefusesUnsafeSymbolsAndNames(t *testing.T) {
	t.Run("target collision", func(t *testing.T) {
		uri := "file:///tmp/schema-rename-collision.nflow"
		source := "@schema Alpha {\n}\n@schema Beta {\n}\n"
		server := newFlowLanguageServer(context.Background(), nil)
		server.initialized = true
		server.documents[uri] = source
		var output bytes.Buffer
		server.out = &output
		response := callSchemaLSPRenameRequest(t, server, &output, uri, source, strings.Index(source, "Alpha"), "Beta")
		if string(response.Result) != "null" {
			t.Fatalf("rename into an existing schema returned %s, want null/no edit", response.Result)
		}
	})

	t.Run("unresolved target collision", func(t *testing.T) {
		uri := "file:///tmp/schema-rename-unresolved-target.nflow"
		source := "@schema Alpha {\n}\nruntime.noop:schema=Future\n"
		server := newFlowLanguageServer(context.Background(), nil)
		server.initialized = true
		server.documents[uri] = source
		var output bytes.Buffer
		server.out = &output
		response := callSchemaLSPRenameRequest(t, server, &output, uri, source, strings.Index(source, "Alpha"), "Future")
		if string(response.Result) != "null" {
			t.Fatalf("rename that would bind an unresolved schema returned %s, want null/no edit", response.Result)
		}
	})

	t.Run("ambiguous declaration", func(t *testing.T) {
		uri := "file:///tmp/schema-rename-ambiguous.nflow"
		source := "@schema Alpha {\n  First string\n}\n@schema Alpha {\n  Second string\n}\n"
		server := newFlowLanguageServer(context.Background(), nil)
		server.initialized = true
		server.documents[uri] = source
		var output bytes.Buffer
		server.out = &output
		response := callSchemaLSPRenameRequest(t, server, &output, uri, source, strings.Index(source, "Alpha"), "Gamma")
		if string(response.Result) != "null" {
			t.Fatalf("rename of multiply declared schema returned %s, want null/no edit", response.Result)
		}
	})

	t.Run("invalid identifier", func(t *testing.T) {
		uri := "file:///tmp/schema-rename-invalid-name.nflow"
		source := "@schema Alpha {\n}\n"
		server := newFlowLanguageServer(context.Background(), nil)
		server.initialized = true
		server.documents[uri] = source
		var output bytes.Buffer
		server.out = &output
		response := callSchemaLSPRenameRequest(t, server, &output, uri, source, strings.Index(source, "Alpha"), "not-valid!")
		var lspError struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(response.Error, &lspError); err != nil {
			t.Fatalf("invalid-name response error = %s: %v", response.Error, err)
		}
		if lspError.Code != -32602 {
			t.Fatalf("invalid-name response code = %d, want -32602", lspError.Code)
		}
	})
}

func callSchemaLSPRenameRequest(t *testing.T, server *flowLanguageServer, output *bytes.Buffer, uri, source string, byteOffset int, newName string) lspMessage {
	t.Helper()
	output.Reset()
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     lspPositionForByteOffset(source, byteOffset),
		"newName":      newName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("4"), Method: "textDocument/rename", Params: params,
	}); err != nil {
		t.Fatal(err)
	}
	messages := readLSPTestMessages(t, output)
	if len(messages) != 1 {
		t.Fatalf("textDocument/rename responses = %d, want 1", len(messages))
	}
	return messages[0]
}
