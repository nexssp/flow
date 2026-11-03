package cli

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

func TestLSPBootstrapsWorkspaceExternalRequirements(t *testing.T) {
	workspace := t.TempDir()
	flowFile := filepath.Join(workspace, "main.nflow")
	if err := os.WriteFile(flowFile, []byte("@require example.com/acme/adapter v1.2.3\ntrue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "invalid.nflow"), []byte("@require\ntrue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(workspace)}).String()

	var input bytes.Buffer
	writeLSPTestMessage(t, &input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"rootUri": rootURI},
	})
	writeLSPTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var (
		ensuredRequirements []require.Requirement
		ensuredFiles        []string
		forwarded           []byte
		ensured             bool
	)
	ensure := func(_ context.Context, requirements []require.Requirement, files ...string) (string, error) {
		ensured = true
		ensuredRequirements = append([]require.Requirement(nil), requirements...)
		ensuredFiles = append([]string(nil), files...)
		return "cached-test-harness", nil
	}
	execute := func(_ context.Context, binary string, stdin io.Reader, stdout io.Writer) int {
		if binary != "cached-test-harness" {
			t.Errorf("harness binary = %q, want cached-test-harness", binary)
		}
		var readErr error
		forwarded, readErr = io.ReadAll(stdin)
		if readErr != nil {
			t.Errorf("read forwarded LSP stream: %v", readErr)
		}
		_, _ = io.WriteString(stdout, "proxied")
		return 23
	}

	var output bytes.Buffer
	var code int
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		code = runLSPWithProjectRequirements(ctx, inv, &input, &output, ensure, execute)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 23 || output.String() != "proxied" {
		t.Fatalf("LSP bootstrap result: code=%d output=%q, want 23 and proxied", code, output.String())
	}
	if !ensured {
		t.Fatal("LSP startup did not prepare a harness for the workspace requirement")
	}
	if len(ensuredRequirements) != 1 || ensuredRequirements[0].Import != "example.com/acme/adapter" || ensuredRequirements[0].Version != "v1.2.3" {
		t.Fatalf("harness requirements = %#v, want example.com/acme/adapter@v1.2.3", ensuredRequirements)
	}
	if len(ensuredFiles) != 1 || ensuredFiles[0] != flowFile {
		t.Fatalf("harness source files = %#v, want [%q]", ensuredFiles, flowFile)
	}
	if !strings.Contains(string(forwarded), `"method":"initialize"`) || !strings.Contains(string(forwarded), `"method":"exit"`) {
		t.Fatalf("harness did not receive the complete LSP stream: %q", forwarded)
	}
}
