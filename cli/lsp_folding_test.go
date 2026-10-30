package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

type lspTestFoldingRange struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Kind      string `json:"kind"`
}

func TestLanguageServerFoldingRanges(t *testing.T) {
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
	if initialized.Capabilities["foldingRangeProvider"] != true {
		t.Fatalf("folding range capability = %#v, want true", initialized.Capabilities["foldingRangeProvider"])
	}

	uri := "file:///tmp/folding.nflow"
	source := "# Heading 😀\n" +
		"@schema Request {\n" +
		"  ID string\n" +
		"  Nested struct {\n" +
		"    Text string\n" +
		"  }\n" +
		"  Note string # }\n" +
		"  Literal string = \"}\"\n" +
		"}\n" +
		"@schema Inline struct { Value int }\n" +
		"# @pipeline Commented\n" +
		"@pipeline submit:timeout=2s\n" +
		"  @schema Hidden {\n" +
		"    Fake string\n" +
		"  }\n" +
		"  runtime.literal:message=\"}\" // @end\n" +
		"@end\n" +
		"\"@pipeline Stringed\"\n"
	server.documents[uri] = source
	server.documentVersion[uri] = 1

	output.Reset()
	params, err := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": uri}})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "textDocument/foldingRange", Params: params,
	}); err != nil {
		t.Fatal(err)
	}
	messages = readLSPTestMessages(t, &output)
	if len(messages) != 1 {
		t.Fatalf("folding-range responses = %d, want 1", len(messages))
	}
	var ranges []lspTestFoldingRange
	if err := json.Unmarshal(messages[0].Result, &ranges); err != nil {
		t.Fatal(err)
	}
	want := []lspTestFoldingRange{
		{StartLine: 1, EndLine: 8, Kind: "region"},
		{StartLine: 11, EndLine: 16, Kind: "region"},
	}
	if len(ranges) != len(want) {
		t.Fatalf("folding ranges = %#v, want %#v", ranges, want)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Errorf("folding range %d = %#v, want %#v", i, ranges[i], want[i])
		}
	}
}

func TestLSPFoldingRangesIncompleteSchemaEndsAtEOF(t *testing.T) {
	source := "@schema Partial {\n  Value string\n  Nested struct {\n    Text string"
	ranges := lspFoldingRanges(source)
	if len(ranges) != 1 {
		t.Fatalf("incomplete schema folding ranges = %#v, want one range", ranges)
	}
	got := ranges[0]
	if got["startLine"] != 0 || got["endLine"] != 3 {
		t.Fatalf("incomplete schema folding range = %#v, want lines 0-3", got)
	}
}

func TestLSPFoldingRangesExcludeInlineBlocks(t *testing.T) {
	source := "@schema Inline struct { Value int }\n@pipeline one"
	if got := lspFoldingRanges(source); len(got) != 0 {
		t.Fatalf("inline/incomplete folding ranges = %#v, want none", got)
	}
}
