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

type lspTestWorkspaceSymbol struct {
	Name     string `json:"name"`
	Kind     int    `json:"kind"`
	Location struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	} `json:"location"`
}

func TestLanguageServerWorkspaceSymbols(t *testing.T) {
	workspace := t.TempDir()
	mainPath := filepath.Join(workspace, "main.nflow")
	diskSource := "@schema DiskVersion { ID string }\n@pipeline disk_pipeline\n  runtime.noop\n@end\n"
	if err := os.WriteFile(mainPath, []byte(diskSource), 0o600); err != nil {
		t.Fatal(err)
	}
	nestedPath := filepath.Join(workspace, "nested", "other.nflow")
	nestedSource := "@schema NestedSchema { Value int }\n@pipeline nested_pipeline\n  runtime.noop\n@end\n"
	if err := os.MkdirAll(filepath.Dir(nestedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nestedPath, []byte(nestedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ignoredDir := range []string{"vendor", ".private", "_partials", "testdata"} {
		path := filepath.Join(workspace, ignoredDir, "ignored.nflow")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("@schema IgnoredSchema {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.nflow")
	if err := os.WriteFile(outsidePath, []byte("@schema OutsideSymlinkSchema {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(workspace, "linked.nflow")); err != nil {
		t.Logf("symlink traversal check unavailable: %v", err)
	}

	mainURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(mainPath)}).String()
	nestedURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(nestedPath)}).String()
	openSource := "@schema OpenSchema { Name string }\n@pipeline open_pipeline\n  runtime.noop\n@end\n"
	untitledURI := "untitled:Scratch"
	untitledSource := "@schema ScratchSchema {}\n"

	var output bytes.Buffer
	server := newFlowLanguageServer(context.Background(), nil)
	server.out = &output
	rootURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(workspace)}).String()
	initializeParams, err := json.Marshal(map[string]any{"rootUri": rootURI})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize",
		Params: initializeParams,
	}); err != nil {
		t.Fatal(err)
	}
	initializeMessages := readLSPTestMessages(t, &output)
	if len(initializeMessages) != 1 {
		t.Fatalf("initialize responses = %d, want 1", len(initializeMessages))
	}
	var initialized struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(initializeMessages[0].Result, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.Capabilities["workspaceSymbolProvider"] != true {
		t.Fatalf("workspace symbol capability = %#v, want true", initialized.Capabilities["workspaceSymbolProvider"])
	}
	server.documents[mainURI] = openSource
	server.documents[untitledURI] = untitledSource

	output.Reset()
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "workspace/symbol",
		Params: json.RawMessage(`{"query":""}`),
	}); err != nil {
		t.Fatal(err)
	}
	messages := readLSPTestMessages(t, &output)
	if len(messages) != 1 {
		t.Fatalf("workspace-symbol responses = %d, want 1", len(messages))
	}
	var symbols []lspTestWorkspaceSymbol
	if err := json.Unmarshal(messages[0].Result, &symbols); err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 5 {
		t.Fatalf("workspace symbol count = %d (%#v), want exactly 5", len(symbols), symbols)
	}
	got := make(map[string][]lspTestWorkspaceSymbol)
	for _, symbol := range symbols {
		got[symbol.Name] = append(got[symbol.Name], symbol)
	}
	for _, name := range []string{"NestedSchema", "nested_pipeline", "OpenSchema", "open_pipeline", "ScratchSchema"} {
		if len(got[name]) != 1 {
			t.Errorf("workspace results for %q = %#v, want exactly one", name, got[name])
		}
	}
	for _, name := range []string{"DiskVersion", "disk_pipeline", "IgnoredSchema", "OutsideSymlinkSchema"} {
		if len(got[name]) != 0 {
			t.Errorf("workspace results unexpectedly include %q: %#v", name, got[name])
		}
	}
	if got["OpenSchema"][0].Location.URI != mainURI {
		t.Errorf("open-buffer symbol URI = %q, want %q", got["OpenSchema"][0].Location.URI, mainURI)
	}
	wantRange := lspRangeForByteOffsets(openSource, strings.Index(openSource, "OpenSchema"), strings.Index(openSource, "OpenSchema")+len("OpenSchema"))
	if got["OpenSchema"][0].Location.Range != wantRange {
		t.Errorf("OpenSchema location range = %#v, want %#v", got["OpenSchema"][0].Location.Range, wantRange)
	}
	if got["NestedSchema"][0].Location.URI != nestedURI {
		t.Errorf("nested file symbol URI = %q, want %q", got["NestedSchema"][0].Location.URI, nestedURI)
	}

	output.Reset()
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("3"), Method: "workspace/symbol",
		Params: json.RawMessage(`{"query":"OPEN"}`),
	}); err != nil {
		t.Fatal(err)
	}
	messages = readLSPTestMessages(t, &output)
	if len(messages) != 1 {
		t.Fatalf("filtered workspace-symbol responses = %d, want 1", len(messages))
	}
	symbols = nil
	if err := json.Unmarshal(messages[0].Result, &symbols); err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 || symbols[0].Name != "OpenSchema" || symbols[1].Name != "open_pipeline" {
		t.Fatalf("case-insensitive workspace query results = %#v, want OpenSchema and open_pipeline", symbols)
	}
}
