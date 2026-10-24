package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
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
