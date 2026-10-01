package fs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestResolveTargetDirs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  WalkConfig
		want []string
	}{
		{"dirs preferred", WalkConfig{Dirs: []string{"a", "b"}, Dir: "c"}, []string{"a", "b"}},
		{"single dir", WalkConfig{Dir: "x"}, []string{"x"}},
		{"default", WalkConfig{}, []string{"."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, resolveTargetDirs(c.cfg), c.want)
		})
	}
}

func TestBuildSkipMap(t *testing.T) {
	t.Parallel()
	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		m := buildSkipMap(WalkConfig{})
		ktest.RequireCondition(t, m[".git"], ".git should be skipped by default")
		ktest.RequireCondition(t, m["node_modules"], "node_modules should be skipped by default")
	})

	t.Run("include all disables defaults", func(t *testing.T) {
		t.Parallel()
		m := buildSkipMap(WalkConfig{IncludeAll: true})
		ktest.RequireEqual(t, len(m), 0)
	})

	t.Run("custom skip added", func(t *testing.T) {
		t.Parallel()
		m := buildSkipMap(WalkConfig{Skip: []string{"custom", "  other  "}})
		ktest.RequireCondition(t, m["custom"], "custom should be skipped")
		ktest.RequireCondition(t, m["other"], "other should be skipped")
	})
}

func TestCalculateDepth(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"":              0,
		".":             0,
		"a":             1,
		"a/b":           2,
		"a/b/c":         3,
		"deep/nested/x": 3,
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, calculateDepth(input), want)
		})
	}
}

func TestWalkSource_RealFilesystem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package a")
	writeFile(t, filepath.Join(root, "b.txt"), "hello")
	writeFile(t, filepath.Join(root, "sub", "c.go"), "package c")
	writeFile(t, filepath.Join(root, ".hidden.go"), "hidden")
	writeFile(t, filepath.Join(root, "node_modules", "dep.go"), "dep")

	t.Run("default skips hidden and defaults", func(t *testing.T) {
		t.Parallel()
		got := collectPaths(t, WalkConfig{Dir: root})
		ktest.RequireCondition(t, contains(got, "a.go"), "expected a.go in %v", got)
		ktest.RequireCondition(t, contains(got, "b.txt"), "expected b.txt in %v", got)
		ktest.RequireCondition(t, contains(got, "sub/c.go"), "expected sub/c.go in %v", got)
		ktest.RequireCondition(t, !contains(got, ".hidden.go"), "hidden file should be skipped")
		ktest.RequireCondition(t, !contains(got, "node_modules/dep.go"), "node_modules should be skipped")
	})

	t.Run("ext filter", func(t *testing.T) {
		t.Parallel()
		got := collectPaths(t, WalkConfig{Dir: root, Ext: "go"})
		for _, p := range got {
			ktest.RequireCondition(t, filepath.Ext(p) == ".go", "%s has wrong ext", p)
		}
	})

	t.Run("include hidden", func(t *testing.T) {
		t.Parallel()
		got := collectPaths(t, WalkConfig{Dir: root, IncludeHidden: true})
		ktest.RequireCondition(t, contains(got, ".hidden.go"), "expected .hidden.go in %v", got)
	})

	t.Run("include all", func(t *testing.T) {
		t.Parallel()
		got := collectPaths(t, WalkConfig{Dir: root, IncludeAll: true})
		ktest.RequireCondition(t, contains(got, "node_modules/dep.go"), "expected dep.go in %v", got)
	})
}

func writeFile(tb testing.TB, path, content string) {
	tb.Helper()
	ktest.RequireNoError(tb, os.MkdirAll(filepath.Dir(path), 0o755))
	ktest.RequireNoError(tb, os.WriteFile(path, []byte(content), 0o600))
}

func collectPaths(tb testing.TB, cfg WalkConfig) []string {
	tb.Helper()
	source := WalkSource()
	stream, err := source.DoStreamAny(context.Background(), cfg)
	ktest.RequireNoError(tb, err)

	var paths []string
	stream(func(item any, itemErr error) bool {
		ktest.RequireNoError(tb, itemErr)
		meta, ok := item.(FileMeta)
		if !ok {
			tb.Fatalf("item type = %T, want FileMeta", item)
		}
		paths = append(paths, meta.RelPath)
		return true
	})
	return paths
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
