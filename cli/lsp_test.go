package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestLanguageServerPublishesCompilerDiagnosticsAndAcceptsVersionDirective(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	invalidSource := "'😀' true"
	validSource := "@nflow_version \">=0.0.0\"\ntrue"
	var input bytes.Buffer
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": "file:///tmp/invalid.nflow", "languageId": "nexss-flow", "version": 1, "text": invalidSource,
		}},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": "file:///tmp/versioned.nflow", "languageId": "nexss-flow", "version": 1, "text": validSource,
		}},
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		return server.serve(&input, &output)
	})
	if err != nil {
		t.Fatal(err)
	}

	messages := readLSPTestMessages(t, &output)
	byURI := make(map[string][]lspDiagnostic)
	for _, message := range messages {
		if message.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var params struct {
			URI         string          `json:"uri"`
			Diagnostics []lspDiagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatal(err)
		}
		byURI[params.URI] = params.Diagnostics
	}

	invalid := byURI["file:///tmp/invalid.nflow"]
	if len(invalid) != 1 {
		t.Fatalf("invalid Flow source diagnostics = %#v, want one compiler diagnostic", invalid)
	}
	if got := invalid[0].Range.Start; got != (lspPosition{Line: 0, Character: 5}) {
		t.Fatalf("compiler diagnostic starts at %#v, want UTF-16 line 0, character 5", got)
	}
	if invalid[0].Source != lspSource || !strings.Contains(invalid[0].Message, `unexpected token "true"`) {
		t.Fatalf("unexpected compiler diagnostic: %#v", invalid[0])
	}
	if got := byURI["file:///tmp/versioned.nflow"]; len(got) != 0 {
		t.Fatalf("valid @nflow_version source diagnostics = %#v, want none", got)
	}
}

func TestLanguageServerFormattingUsesCoreFormatter(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "@pipeline work:timeout=2s:retry=3\n  action.node:retry=2\n@end"
	var input bytes.Buffer
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": "file:///tmp/format.nflow", "languageId": "nexss-flow", "version": 1, "text": source,
		}},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/formatting",
		"params": map[string]any{"textDocument": map[string]any{"uri": "file:///tmp/format.nflow"}},
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	var expected string
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		coreFormatter, formatErr := formatterFor(ctx, inv, source, "/tmp/format.nflow")
		if formatErr != nil {
			return formatErr
		}
		expected, formatErr = coreFormatter.Format(ctx, source, "/tmp/format.nflow")
		if formatErr != nil {
			return formatErr
		}
		server := newFlowLanguageServer(ctx, inv)
		return server.serve(&input, &output)
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, message := range readLSPTestMessages(t, &output) {
		if string(message.ID) != "2" {
			continue
		}
		var edits []struct {
			NewText string `json:"newText"`
		}
		if err := json.Unmarshal(message.Result, &edits); err != nil {
			t.Fatal(err)
		}
		if len(edits) != 1 || edits[0].NewText != expected {
			t.Fatalf("LSP formatting result = %#v, want core formatter output %q", edits, expected)
		}
		return
	}
	t.Fatal("language server did not answer the formatting request")
}

func TestLanguageServerContinuesAfterMalformedMessages(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	writeLSPTestBody(t, &input, []byte(`{"jsonrpc":"2.0","id":99,"method":`))
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/formatting",
		"params": map[string]any{"textDocument": "not an object"},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "textDocument/formatting", "params": map[string]any{},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": "not an object",
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		return server.serve(&input, &output)
	})
	if err != nil {
		t.Fatalf("language server stopped after malformed input: %v", err)
	}

	responses := make(map[string]struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	})
	initialized := false
	for _, message := range readLSPTestMessages(t, &output) {
		if string(message.ID) == "1" && len(message.Result) > 0 {
			initialized = true
		}
		if len(message.Error) > 0 {
			var response struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(message.Error, &response); err != nil {
				t.Fatal(err)
			}
			responses[string(message.ID)] = response
		}
	}
	if !initialized {
		t.Fatal("server did not answer initialize after malformed input")
	}
	if got := responses["null"].Code; got != -32700 {
		t.Fatalf("malformed JSON error code = %d, want -32700", got)
	}
	for _, id := range []string{"2", "3"} {
		if got := responses[id].Code; got != -32602 {
			t.Fatalf("invalid params response %s code = %d, want -32602", id, got)
		}
	}
}

func TestLanguageServerDiscoversRequirementsInIncludedSource(t *testing.T) {
	registerLSPNestedRequirementBundle()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.nflow")
	child := "@require " + lspNestedRequirementBundleID + "\n@pipeline nested_chain\n  runtime.noop\n@end\n"
	if err := os.WriteFile(filepath.Join(dir, "nested.nflow"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String()
	source := "@include \"nested.nflow\" AS nested\nnested.pipeline.nested_chain\n"
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		if diagnostics := server.compileDiagnostics(uri, source); len(diagnostics) != 0 {
			t.Fatalf("nested @require diagnostics = %#v, want none", diagnostics)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLanguageServerExplainsExternalRequirementLimitation(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		diagnostics := server.compileDiagnostics("file:///tmp/external.nflow", "@require github.com/acme/fancy v1.2.3\ntrue\n")
		if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "nflow lint") {
			t.Fatalf("external @require diagnostics = %#v, want explicit nflow lint guidance", diagnostics)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLanguageServerFormattingUsesActivePrimaryExtensions(t *testing.T) {
	bundle := compiler.Bundle{
		ID:        "lsp-test-custom-primary",
		Primaries: []compiler.PrimaryExtension{lspTestPrimary{}},
	}
	inv, err := newNativeInvocation([]compiler.Bundle{bundle}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "lsp_test true"
	var input bytes.Buffer
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": "file:///tmp/custom-primary.nflow", "languageId": "nexss-flow", "version": 1, "text": source,
		}},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/formatting",
		"params": map[string]any{"textDocument": map[string]any{"uri": "file:///tmp/custom-primary.nflow"}},
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		server := newFlowLanguageServer(ctx, inv)
		return server.serve(&input, &output)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range readLSPTestMessages(t, &output) {
		if string(message.ID) != "2" {
			continue
		}
		if len(message.Error) > 0 {
			t.Fatalf("custom-primary formatting returned error: %s", message.Error)
		}
		var edits []struct {
			NewText string `json:"newText"`
		}
		if err := json.Unmarshal(message.Result, &edits); err != nil {
			t.Fatal(err)
		}
		if len(edits) != 1 {
			t.Fatalf("custom-primary formatting edits = %#v", edits)
		}
		return
	}
	t.Fatal("language server did not answer custom-primary formatting request")
}

func TestLanguageServerUsesRootURIForUntitledDocuments(t *testing.T) {
	workspace := t.TempDir()
	rootURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(workspace)}).String()
	for _, params := range []map[string]any{
		{"rootUri": rootURI},
		{"rootUri": nil, "workspaceFolders": []any{map[string]any{"uri": rootURI}}},
	} {
		var input, output bytes.Buffer
		writeLSPTestMessage(t, &input, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": params,
		})
		server := &flowLanguageServer{ctx: context.Background(), documents: make(map[string]string)}
		if err := server.serve(&input, &output); err != nil {
			t.Fatal(err)
		}
		if got, want := server.sourceName("untitled:Untitled-1"), filepath.Join(workspace, "Untitled-1.nflow"); got != want {
			t.Fatalf("untitled source path = %q, want %q", got, want)
		}
	}
}

func TestRunLSPHelpWorksWithoutStartingServer(t *testing.T) {
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	code := runLSP(context.Background(), nil, []string{"--help"})
	_ = writer.Close()
	os.Stdout = original
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if code != 0 || !strings.Contains(string(output), "nflow lsp") {
		t.Fatalf("nflow lsp --help: exit=%d output=%q", code, output)
	}
}

const lspNestedRequirementBundleID = "nflow_lsp_nested_test_bundle"

var lspNestedRequirementBundleOnce sync.Once

func registerLSPNestedRequirementBundle() {
	lspNestedRequirementBundleOnce.Do(func() {
		compiler.Register(lspNestedRequirementBundleID, func(map[string]string) compiler.Bundle {
			return compiler.Bundle{ID: lspNestedRequirementBundleID}
		})
	})
}

func writeLSPTestMessage(t *testing.T, output io.Writer, message any) {
	t.Helper()
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	writeLSPTestBody(t, output, body)
}

func writeLSPTestBody(t *testing.T, output io.Writer, body []byte) {
	t.Helper()
	if _, err := fmt.Fprintf(output, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write(body); err != nil {
		t.Fatal(err)
	}
}

func readLSPTestMessages(t *testing.T, input io.Reader) []lspMessage {
	t.Helper()
	reader := bufio.NewReader(input)
	var messages []lspMessage
	for {
		body, err := readLSPFrame(reader)
		if errors.Is(err, io.EOF) {
			return messages
		}
		if err != nil {
			t.Fatal(err)
		}
		var message lspMessage
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
}
