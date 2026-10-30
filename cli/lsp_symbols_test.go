package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type lspTestDocumentSymbol struct {
	Name           string   `json:"name"`
	Detail         string   `json:"detail"`
	Kind           int      `json:"kind"`
	Range          lspRange `json:"range"`
	SelectionRange lspRange `json:"selectionRange"`
}

func TestLanguageServerDocumentSymbols(t *testing.T) {
	server := newFlowLanguageServer(context.Background(), nil)
	var output bytes.Buffer
	server.out = &output
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	messages := readLSPTestMessages(t, &output)
	if len(messages) != 1 {
		t.Fatalf("initialize responses = %d, want 1", len(messages))
	}
	var initialized struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(messages[0].Result, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.Capabilities["documentSymbolProvider"] != true {
		t.Fatalf("document symbol capability = %#v, want true", initialized.Capabilities["documentSymbolProvider"])
	}

	uri := "file:///tmp/outline.nflow"
	source := "# Heading 😀\n" +
		"@schema Request {\n" +
		"  ID string\n" +
		"}\n" +
		"@pipeline submit:timeout=2s\n" +
		"  @schema Hidden {\n" +
		"    Fake string\n" +
		"  }\n" +
		"  runtime.noop\n" +
		"@end\n" +
		"@schema Inline struct { Value int }\n" +
		"# @pipeline Commented\n" +
		"\"@schema Stringed { No string }\"\n"
	server.documents[uri] = source
	server.documentVersion[uri] = 1

	output.Reset()
	params, err := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": uri}})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "textDocument/documentSymbol", Params: params,
	}); err != nil {
		t.Fatal(err)
	}
	messages = readLSPTestMessages(t, &output)
	if len(messages) != 1 {
		t.Fatalf("document-symbol responses = %d, want 1", len(messages))
	}
	var symbols []lspTestDocumentSymbol
	if err := json.Unmarshal(messages[0].Result, &symbols); err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 3 {
		t.Fatalf("document symbols = %#v, want only Request, submit, and Inline", symbols)
	}

	expectations := []struct {
		name           string
		detail         string
		kind           int
		rangeStart     int
		rangeEnd       int
		selectionStart int
	}{
		{
			name: "Request", detail: "Schema", kind: lspSymbolKindStruct,
			rangeStart:     strings.Index(source, "@schema Request"),
			rangeEnd:       strings.Index(source, "}\n") + 1,
			selectionStart: strings.Index(source, "Request"),
		},
		{
			name: "submit", detail: "Pipeline", kind: lspSymbolKindFunction,
			rangeStart:     strings.Index(source, "@pipeline submit"),
			rangeEnd:       strings.Index(source, "@end") + len("@end"),
			selectionStart: strings.Index(source, "submit"),
		},
		{
			name: "Inline", detail: "Schema", kind: lspSymbolKindStruct,
			rangeStart:     strings.Index(source, "@schema Inline"),
			rangeEnd:       strings.Index(source, "Value int }") + len("Value int }"),
			selectionStart: strings.Index(source, "Inline"),
		},
	}
	for i, want := range expectations {
		got := symbols[i]
		if got.Name != want.name || got.Detail != want.detail || got.Kind != want.kind {
			t.Errorf("symbol %d = %#v, want %s %s (kind %d)", i, got, want.detail, want.name, want.kind)
		}
		wantRange := lspRangeForByteOffsets(source, want.rangeStart, want.rangeEnd)
		if got.Range != wantRange {
			t.Errorf("%s range = %#v, want %#v", want.name, got.Range, wantRange)
		}
		wantSelection := lspRangeForByteOffsets(source, want.selectionStart, want.selectionStart+len(want.name))
		if got.SelectionRange != wantSelection {
			t.Errorf("%s selection range = %#v, want %#v", want.name, got.SelectionRange, wantSelection)
		}
	}
}

func TestLSPDocumentSymbolsIncompleteSchemaEndsAtEOF(t *testing.T) {
	source := "@schema Partial {\n  Value string"
	symbols := lspDocumentSymbols(source)
	if len(symbols) != 1 || symbols[0]["name"] != "Partial" {
		t.Fatalf("incomplete schema symbols = %#v, want Partial", symbols)
	}
	got, ok := symbols[0]["range"].(lspRange)
	if !ok {
		t.Fatalf("incomplete schema range has type %T, want lspRange", symbols[0]["range"])
	}
	want := lspRangeForByteOffsets(source, 0, len(source))
	if got != want {
		t.Fatalf("incomplete schema range = %#v, want EOF range %#v", got, want)
	}
}
