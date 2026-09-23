package fsio_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/nodes/fsio"
	"github.com/nexssp/kernel/action"
)

func createTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	structure := []string{
		"main.go",
		"README.md",
		".hidden.txt",
		"pkg/auth/auth.go",
		"pkg/auth/token.ts",
		"pkg/auth/auth_test.go",
		"node_modules/bad_pkg/index.js",
		"vendor/dep/dep.go",
		".git/config",
		"build/output.bin",
		"deep/sub1/sub2/deep_file.go",
	}

	for _, path := range structure {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("package test"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func collectMeta(t *testing.T, source action.AnyStreamAction, req fsio.WalkReq) []fsio.FileMeta {
	t.Helper()
	anyStream, err := source.DoStreamAny(context.Background(), req)
	if err != nil {
		t.Fatalf("DoStreamAny failed: %v", err)
	}

	var out []fsio.FileMeta
	anyStream(func(item any, itemErr error) bool {
		if itemErr != nil {
			t.Fatalf("stream yielded error: %v", itemErr)
			return false
		}
		out = append(out, item.(fsio.FileMeta))
		return true
	})
	return out
}

func TestWalk_DefaultSkipDirs(t *testing.T) {
	t.Parallel()
	ws := createTestWorkspace(t)
	src := fsio.WalkSource()

	items := collectMeta(t, src, fsio.WalkReq{Dir: ws})

	for _, item := range items {
		for _, skipped := range []string{"node_modules", "vendor", ".git", "build"} {
			if stringsContainsSegment(item.RelPath, skipped) {
				t.Fatalf("file %q should have been skipped by DefaultSkipDirs (%s)", item.RelPath, skipped)
			}
		}
	}
}

func TestWalk_IncludeAll(t *testing.T) {
	t.Parallel()
	ws := createTestWorkspace(t)
	src := fsio.WalkSource()

	items := collectMeta(t, src, fsio.WalkReq{Dir: ws, IncludeAll: true})

	foundVendor := false
	for _, item := range items {
		if stringsContainsSegment(item.RelPath, "vendor") {
			foundVendor = true
			break
		}
	}
	if !foundVendor {
		t.Fatal("expected vendor directory to be visited when IncludeAll is true")
	}
}

func TestWalk_ExtFilter(t *testing.T) {
	t.Parallel()
	ws := createTestWorkspace(t)
	src := fsio.WalkSource()

	items := collectMeta(t, src, fsio.WalkReq{Dir: ws, Ext: "go"})

	if len(items) == 0 {
		t.Fatal("expected to find .go files")
	}

	for _, item := range items {
		if filepath.Ext(item.Path) != ".go" {
			t.Fatalf("expected only .go files, got %q", item.RelPath)
		}
	}
}

func TestWalk_MaxDepth(t *testing.T) {
	t.Parallel()
	ws := createTestWorkspace(t)
	src := fsio.WalkSource()

	// MaxDepth 1: only root files (main.go, README.md, etc.)
	items := collectMeta(t, src, fsio.WalkReq{Dir: ws, MaxDepth: 1, IncludeHidden: false})

	for _, item := range items {
		if filepath.Dir(item.RelPath) != "." {
			t.Fatalf("file %q exceeds MaxDepth=1", item.RelPath)
		}
	}
}

func TestWalk_BackpressureCancellation(t *testing.T) {
	t.Parallel()
	ws := createTestWorkspace(t)
	src := fsio.WalkSource()

	anyStream, err := src.DoStreamAny(context.Background(), fsio.WalkReq{Dir: ws})
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	anyStream(func(item any, itemErr error) bool {
		count++
		return count < 2 // break after consuming 2 items
	})

	if count != 2 {
		t.Fatalf("expected stream to stop at 2 items via backpressure, visited %d", count)
	}
}

func TestWalk_ContextCancellation(t *testing.T) {
	t.Parallel()
	ws := createTestWorkspace(t)
	src := fsio.WalkSource()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	anyStream, err := src.DoStreamAny(ctx, fsio.WalkReq{Dir: ws})
	if err != nil {
		t.Fatal(err)
	}

	sawErr := false
	anyStream(func(_ any, itemErr error) bool {
		if itemErr != nil {
			sawErr = true
			return false
		}
		return true
	})

	if !sawErr {
		t.Fatal("expected context.Canceled error to be yielded")
	}
}

func stringsContainsSegment(relPath, segment string) bool {
	parts := filepath.SplitList(filepath.ToSlash(relPath))
	_ = parts
	rel := filepath.ToSlash(relPath)
	return rel == segment ||
		strings.HasPrefix(rel, segment+"/") ||
		strings.Contains(rel, "/"+segment+"/")
}
