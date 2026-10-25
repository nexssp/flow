package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const lspProtocolChildEnv = "FLOW_LSP_PROTOCOL_CHILD"

func TestLSPProtocolSubprocessHelper(_ *testing.T) {
	if os.Getenv(lspProtocolChildEnv) != "1" {
		return
	}
	os.Exit(RunContext(context.Background(), []string{"lsp"}))
}

func TestLSPStdioChildProcessProtocolIntegration(t *testing.T) {
	const uri = "file:///tmp/lsp-protocol.nflow"
	const invalidSource = "'😀' true"
	var input bytes.Buffer
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": nil, "method": "initialize",
		"params": map[string]any{"rootUri": "file:///tmp"},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": "file:///tmp/malformed-open.nflow", "version": 1,
		}},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": "nexss-flow", "version": 1, "text": "true",
		}},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": 2},
			"contentChanges": []any{map[string]any{"text": invalidSource}},
		},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": 1},
			"contentChanges": []any{map[string]any{"text": "false"}},
		},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": 3},
			"contentChanges": []any{map[string]any{
				"range": map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 4}},
				"text":  "false",
			}},
		},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": 3},
			"contentChanges": []any{map[string]any{"text": "true"}},
		},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": uri, "version": 4, "text": invalidSource,
		}},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": "format", "method": "textDocument/formatting",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri}},
	})
	writeLSPTestBody(t, &input, []byte(`{"jsonrpc":"2.0","id":99,"method":`))
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "id": 77})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 78, "method": "textDocument/formatting", "params": "not-an-object",
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument":   map[string]any{"uri": uri},
			"contentChanges": []any{map[string]any{"text": "false"}},
		},
	})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": "recover", "method": "textDocument/completion",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 0}},
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "id": nil, "method": "shutdown"})
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 79, "method": "textDocument/hover",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 0}},
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	//nolint:gosec // Launches this test binary only, with a fixed test filter, to exercise real stdio.
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLSPProtocolSubprocessHelper$")
	command.Env = append(os.Environ(), lspProtocolChildEnv+"=1")
	command.Stdin = bytes.NewReader(input.Bytes())
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("LSP child process failed: %v; stderr: %s", err, stderr.String())
	}

	messages := readLSPTestMessages(t, &stdout)
	var initNullID, shutdownNullID, recovered bool
	var formattingText string
	var diagnosticsVersions []int
	var diagnosticPayloads []json.RawMessage
	responsesByID := make(map[string]struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	})
	for _, message := range messages {
		if string(message.ID) == "null" {
			if len(message.Result) > 0 {
				var result map[string]json.RawMessage
				if err := json.Unmarshal(message.Result, &result); err == nil && len(result["capabilities"]) > 0 {
					initNullID = true
				} else if string(message.Result) == "null" {
					shutdownNullID = true
				}
			}
			if len(message.Error) > 0 {
				var response struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}
				if err := json.Unmarshal(message.Error, &response); err != nil {
					t.Fatal(err)
				}
				responsesByID[strconv.Itoa(response.Code)] = response
			}
		}
		if string(message.ID) == "78" && len(message.Error) > 0 {
			var response struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(message.Error, &response); err != nil {
				t.Fatal(err)
			}
			responsesByID["78"] = response
		}
		if string(message.ID) == "79" && len(message.Error) > 0 {
			var response struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(message.Error, &response); err != nil {
				t.Fatal(err)
			}
			responsesByID["79"] = response
		}
		if string(message.ID) == `"recover"` && len(message.Result) > 0 {
			recovered = true
		}
		if string(message.ID) == `"format"` {
			if len(message.Error) > 0 {
				t.Fatalf("formatting after duplicate open failed: %s", message.Error)
			}
			var edits []struct {
				NewText string `json:"newText"`
			}
			if err := json.Unmarshal(message.Result, &edits); err != nil {
				t.Fatal(err)
			}
			if len(edits) != 1 {
				t.Fatalf("formatting edits = %#v, want one full-document edit", edits)
			}
			formattingText = edits[0].NewText
		}
		if message.Method == "textDocument/publishDiagnostics" {
			var params struct {
				URI         string          `json:"uri"`
				Version     int             `json:"version"`
				Diagnostics json.RawMessage `json:"diagnostics"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				t.Fatal(err)
			}
			if params.URI == uri {
				diagnosticsVersions = append(diagnosticsVersions, params.Version)
				diagnosticPayloads = append(diagnosticPayloads, params.Diagnostics)
			}
		}
		if len(message.Error) > 0 {
			var response struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(message.Error, &response); err != nil {
				t.Fatal(err)
			}
			if response.Code == -32700 || response.Code == -32600 {
				responsesByID[strconv.Itoa(response.Code)] = response
			}
		}
	}
	if !initNullID || !shutdownNullID {
		t.Fatalf("null-ID initialize/shutdown responses missing: initialize=%t shutdown=%t", initNullID, shutdownNullID)
	}
	if !recovered {
		t.Fatal("server did not recover to answer a request after malformed JSON/requests/notifications")
	}
	if strings.TrimSpace(formattingText) != "true" {
		t.Fatalf("formatting used overwritten/stale document text: %q", formattingText)
	}
	if got := responsesByID["-32700"].Code; got != -32700 {
		t.Fatalf("malformed JSON code = %d, want -32700", got)
	}
	if got := responsesByID["-32600"].Code; got != -32600 {
		t.Fatalf("invalid request code = %d, want -32600", got)
	}
	if got := responsesByID["78"].Code; got != -32602 {
		t.Fatalf("invalid params request code = %d, want -32602", got)
	}
	if got := responsesByID["79"].Code; got != -32600 {
		t.Fatalf("post-shutdown request code = %d, want -32600", got)
	}
	if len(diagnosticsVersions) != 3 || diagnosticsVersions[0] != 1 || diagnosticsVersions[1] != 2 || diagnosticsVersions[2] != 3 {
		t.Fatalf("diagnostic versions = %v, want [1 2 3] with stale/malformed/duplicate updates ignored", diagnosticsVersions)
	}
	if string(diagnosticPayloads[0]) != "[]" {
		t.Fatalf("valid open diagnostics = %s, want explicit empty array", diagnosticPayloads[0])
	}
	if strings.TrimSpace(string(diagnosticPayloads[1])) == "[]" || strings.TrimSpace(string(diagnosticPayloads[1])) == "null" {
		t.Fatalf("invalid change diagnostics = %s, want one compiler diagnostic", diagnosticPayloads[1])
	}
	if string(diagnosticPayloads[2]) != "[]" {
		t.Fatalf("valid change did not clear diagnostics with []: %s", diagnosticPayloads[2])
	}
}
