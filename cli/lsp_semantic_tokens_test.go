package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type lspTestSemanticToken struct {
	start     lspPosition
	length    int
	tokenType int
	modifiers int
}

func TestLanguageServerSemanticTokens(t *testing.T) {
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
		Capabilities struct {
			SemanticTokensProvider struct {
				Legend struct {
					TokenTypes     []string `json:"tokenTypes"`
					TokenModifiers []string `json:"tokenModifiers"`
				} `json:"legend"`
				Full bool `json:"full"`
			} `json:"semanticTokensProvider"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(messages[0].Result, &initialized); err != nil {
		t.Fatal(err)
	}
	provider := initialized.Capabilities.SemanticTokensProvider
	if !provider.Full || strings.Join(provider.Legend.TokenTypes, ",") != "keyword,function,class" || strings.Join(provider.Legend.TokenModifiers, ",") != "declaration" {
		t.Fatalf("semantic-token capability = %#v, want full support with keyword/function/class and declaration legend", provider)
	}

	const uri = "file:///tmp/semantic-tokens.nflow"
	source := "# @pipeline Ghost\n" +
		"@schema Request {\n" +
		"  Shared\n" +
		"  ID string\n" +
		"}\n" +
		"@schema Shared { Value string }\n" +
		"@pipeline submit\n" +
		"  \"😀\" :request-schema=!Request # :response-schema=Ghost\n" +
		"  \"inside :schema=Ghost\"\n" +
		"@end\n"
	server.documents[uri] = source
	server.documentVersion[uri] = 1

	params, err := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": uri}})
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := server.handle(lspMessage{
		JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "textDocument/semanticTokens/full", Params: params,
	}); err != nil {
		t.Fatal(err)
	}
	messages = readLSPTestMessages(t, &output)
	if len(messages) != 1 {
		t.Fatalf("semantic-token responses = %d, want 1", len(messages))
	}
	var result struct {
		Data []int `json:"data"`
	}
	if err := json.Unmarshal(messages[0].Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data)%5 != 0 {
		t.Fatalf("semantic-token data length = %d, want a multiple of five", len(result.Data))
	}

	want := []lspTestSemanticToken{
		semanticTokenAt(source, strings.Index(source, "@schema Request"), "@schema", lspSemanticTokenKeyword, 0),
		semanticTokenAt(source, strings.Index(source, "Request"), "Request", lspSemanticTokenClass, lspSemanticTokenDeclaration),
		semanticTokenAt(source, strings.Index(source, "Shared\n"), "Shared", lspSemanticTokenClass, 0),
		semanticTokenAt(source, strings.Index(source, "@schema Shared"), "@schema", lspSemanticTokenKeyword, 0),
		semanticTokenAt(source, strings.Index(source, "Shared {"), "Shared", lspSemanticTokenClass, lspSemanticTokenDeclaration),
		semanticTokenAt(source, strings.Index(source, "@pipeline submit"), "@pipeline", lspSemanticTokenKeyword, 0),
		semanticTokenAt(source, strings.Index(source, "submit"), "submit", lspSemanticTokenFunction, lspSemanticTokenDeclaration),
		semanticTokenAt(source, strings.Index(source, "!Request")+1, "Request", lspSemanticTokenClass, 0),
	}
	got := decodeLSPTestSemanticTokens(t, result.Data)
	if len(got) != len(want) {
		t.Fatalf("semantic tokens = %#v, want %d tokens (no comment/string tokens)", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("semantic token %d = %#v, want %#v", i, got[i], want[i])
		}
	}

	// The reference follows a supplementary Unicode character on its line. Its
	// LSP character position must count UTF-16 code units, not UTF-8 bytes.
	requestReference := want[len(want)-1]
	if requestReference.start.Character != lspPositionForByteOffset(source, strings.Index(source, "!Request")+1).Character {
		t.Fatalf("UTF-16 reference position = %#v, want character %d", requestReference.start, lspPositionForByteOffset(source, strings.Index(source, "!Request")+1).Character)
	}
}

func semanticTokenAt(source string, start int, text string, tokenType, modifiers int) lspTestSemanticToken {
	return lspTestSemanticToken{
		start:     lspPositionForByteOffset(source, start),
		length:    utf16Length(text),
		tokenType: tokenType,
		modifiers: modifiers,
	}
}

func decodeLSPTestSemanticTokens(t *testing.T, data []int) []lspTestSemanticToken {
	t.Helper()
	if len(data)%5 != 0 {
		t.Fatalf("semantic-token data = %v, want groups of five integers", data)
	}
	tokens := make([]lspTestSemanticToken, 0, len(data)/5)
	line, character := 0, 0
	for i := 0; i < len(data); i += 5 {
		deltaLine, deltaCharacter := data[i], data[i+1]
		line += deltaLine
		if deltaLine == 0 {
			character += deltaCharacter
		} else {
			character = deltaCharacter
		}
		tokens = append(tokens, lspTestSemanticToken{
			start:     lspPosition{Line: line, Character: character},
			length:    data[i+2],
			tokenType: data[i+3],
			modifiers: data[i+4],
		})
	}
	return tokens
}
