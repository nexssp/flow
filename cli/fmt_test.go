package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestFmtWriteDoesNotRewriteInvalidSource(t *testing.T) {
	original := []byte("@pipeline create_task:schema=TaskRequest\n  runtime.noop\n@end\n")
	path := filepath.Join(t.TempDir(), "legacy.nflow")
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
	if code == 0 {
		t.Fatal("fmt --write succeeded for a deprecated :schema modifier")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after fmt --write: %v", err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("fmt --write changed invalid source:\n got %q\nwant %q", after, original)
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
