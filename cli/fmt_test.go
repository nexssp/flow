package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
)

func TestFmtWriteAcceptsSharedPipelineSchemaAlias(t *testing.T) {
	original := []byte("@schema TaskRequest {\n  Name string `json:\"name\"`\n}\n\n@pipeline create_task:schema=TaskRequest\n  runtime.noop\n@end\n")
	path := filepath.Join(t.TempDir(), "shared-schema.nflow")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatalf("newNativeInvocation: %v", err)
	}
	code := runInvocation(context.Background(), inv, func(ctx context.Context) int {
		return runFmt(ctx, inv, []string{"--write", path})
	})
	if code != 0 {
		t.Fatal("fmt --write rejected a valid shared :schema modifier")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after fmt --write: %v", err)
	}
	if !bytes.Contains(after, []byte(":schema=TaskRequest")) {
		t.Fatalf("fmt --write removed the shared schema alias: %q", after)
	}
}

func TestFmtUsesRequiredPrimaryExtensionsForFilesAndStdin(t *testing.T) {
	const bundleID = "cli-fmt-custom-primary"
	bundle := compiler.Bundle{
		ID:        bundleID,
		Primaries: []compiler.PrimaryExtension{lspTestPrimary{}},
	}
	source := "@require " + bundleID + "\nlsp_test true\n"

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "custom-primary.nflow")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		inv, err := newNativeInvocation([]compiler.Bundle{bundle}, []string{bundleID})
		if err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := captureLintOutput(t, func() int {
			return runInvocation(context.Background(), inv, func(ctx context.Context) int {
				return runFmt(ctx, inv, []string{path})
			})
		})
		if code != 0 || stderr != "" || !strings.Contains(stdout, "lsp_test true") {
			t.Fatalf("fmt file with required primary: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("stdin", func(t *testing.T) {
		input, err := os.CreateTemp(t.TempDir(), "stdin-*.nflow")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := input.WriteString(source); err != nil {
			t.Fatal(err)
		}
		if _, err := input.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		previousStdin := os.Stdin
		os.Stdin = input
		defer func() { os.Stdin = previousStdin; _ = input.Close() }()

		inv, err := newNativeInvocation([]compiler.Bundle{bundle}, []string{bundleID})
		if err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := captureLintOutput(t, func() int {
			return runInvocation(context.Background(), inv, func(ctx context.Context) int {
				return runFmt(ctx, inv, []string{"--stdin"})
			})
		})
		if code != 0 || stderr != "" || !strings.Contains(stdout, "lsp_test true") {
			t.Fatalf("fmt stdin with required primary: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})
}

func TestFmtUsesSharedHarnessForNamedExternalRequirements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "external.nflow")
	if err := os.WriteFile(path, []byte("@require example.com/acme/format v1.2.3\ntrue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	options := fmtOptions{writeMode: true, files: []string{path}}
	var (
		ensuredRequirements []require.Requirement
		ensuredFiles        []string
		executedArgs        []string
		code                int
		handled             bool
	)
	ensure := func(_ context.Context, requirements []require.Requirement, files ...string) (string, error) {
		ensuredRequirements = append([]require.Requirement(nil), requirements...)
		ensuredFiles = append([]string(nil), files...)
		return "cached-test-harness", nil
	}
	execute := func(_ context.Context, binary string, args []string) int {
		if binary != "cached-test-harness" {
			t.Errorf("harness binary = %q, want cached-test-harness", binary)
		}
		executedArgs = append([]string(nil), args...)
		return 17
	}
	err = inv.host.Run(t.Context(), func(ctx context.Context) error {
		code, handled = formatWithExternalHarnessUsing(ctx, inv, []string{"--write", path}, options, ensure, execute)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || code != 17 {
		t.Fatalf("external fmt handoff: handled=%t code=%d, want true and 17", handled, code)
	}
	if len(ensuredRequirements) != 1 || ensuredRequirements[0].Import != "example.com/acme/format" || ensuredRequirements[0].Version != "v1.2.3" {
		t.Fatalf("harness requirements = %#v, want example.com/acme/format@v1.2.3", ensuredRequirements)
	}
	if len(ensuredFiles) != 1 || ensuredFiles[0] != path {
		t.Fatalf("harness source files = %#v, want [%q]", ensuredFiles, path)
	}
	if len(executedArgs) != 3 || executedArgs[0] != "fmt" || executedArgs[1] != "--write" || executedArgs[2] != path {
		t.Fatalf("harness fmt arguments = %#v, want [fmt --write %s]", executedArgs, path)
	}
}

func TestFmtStdinResolvesRelativeRequireFromWorkingDirectory(t *testing.T) {
	const bundleID = "cli-fmt-relative-require-primary"
	moduleRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module example.com/fmtstdin\n\ngo 1.26.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(moduleRoot, "relative", bundleID), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(moduleRoot)

	bundle := compiler.Bundle{
		ID:        bundleID,
		Primaries: []compiler.PrimaryExtension{lspTestPrimary{}},
	}
	inv, err := newNativeInvocation([]compiler.Bundle{bundle}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.CreateTemp(t.TempDir(), "stdin-*.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.WriteString("@require ./relative/" + bundleID + "\nlsp_test true\n"); err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	previousStdin := os.Stdin
	os.Stdin = input
	t.Cleanup(func() {
		os.Stdin = previousStdin
		_ = input.Close()
	})

	stdout, stderr, code := captureLintOutput(t, func() int {
		return runInvocation(context.Background(), inv, func(ctx context.Context) int {
			return runFmt(ctx, inv, []string{"--stdin"})
		})
	})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "lsp_test true") {
		t.Fatalf("fmt stdin with relative @require: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestFmtUsesSharedHelpArgumentConvention(t *testing.T) {
	stdout, stderr, code := captureLintOutput(t, func() int {
		return runFmt(context.Background(), nil, []string{flagHelpWord})
	})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "nflow fmt") {
		t.Fatalf("fmt help result: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	stdout, stderr, code = captureLintOutput(t, func() int {
		return runFmt(context.Background(), nil, []string{"source.nflow", flagHelp})
	})
	if code != 2 || stdout != "" || !strings.Contains(stderr, "unknown flag") {
		t.Fatalf("help after a positional argument: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
