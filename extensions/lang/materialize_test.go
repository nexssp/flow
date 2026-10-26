package lang

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestMaterializeWritesFile asserts the source is written to a temp dir
// with the expected filename (id + ext), and cleanup removes it.
func TestMaterializeWritesFile(t *testing.T) {
	lang := Language{ID: "python", Ext: ".py"}
	dir, path, err := materialize(context.Background(), &lang, "print(1)")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	defer cleanupMaterialized(dir)

	if !filepath.IsAbs(dir) {
		t.Errorf("materialize dir %q is not absolute", dir)
	}
	if filepath.Base(path) != "python.py" {
		t.Errorf("materialize path = %q; want basename python.py", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "print(1)" {
		t.Errorf("materialized content = %q; want 'print(1)'", got)
	}
}

// TestCleanupMaterializedIdempotent asserts calling cleanup twice is safe.
func TestCleanupMaterializedIdempotent(t *testing.T) {
	lang := Language{ID: "python", Ext: ".py"}
	dir, _, err := materialize(context.Background(), &lang, "x")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	cleanupMaterialized(dir)
	cleanupMaterialized(dir) // must not panic
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("after cleanup, dir %q still exists: %v", dir, err)
	}
}

// TestCleanupMaterializedEmpty is a no-op assertion.
func TestCleanupMaterializedEmpty(_ *testing.T) {
	cleanupMaterialized("") // must not panic
}

// TestMaterializeCtxCancelled verifies that a canceled execution cannot
// create a temp file that would need asynchronous cleanup.
func TestMaterializeCtxCancelled(t *testing.T) {
	lang := Language{ID: "python", Ext: ".py"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if dir, path, err := materialize(ctx, &lang, "x"); err == nil {
		cleanupMaterialized(dir)
		t.Fatalf("materialize(%q) unexpectedly created %q with a canceled context", dir, path)
	}
}
