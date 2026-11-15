package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

func TestCopyLoosePackageRejectsSymlinkedGoSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(source, []byte("package escape\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(root, "linked.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := copyLoosePackage(require.Requirement{LocalPath: root}, filepath.Join(t.TempDir(), "copied"))
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("copyLoosePackage error = %v; want symlink rejection", err)
	}
}

func TestCopyLoosePackageRejectsSymlinkedAdapterDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "library.go"), []byte("package adapter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, defaultAdapterDirectory)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := copyLoosePackage(require.Requirement{LocalPath: root}, filepath.Join(t.TempDir(), "copied"))
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("copyLoosePackage error = %v; want symlink rejection", err)
	}
}

func TestCopyLoosePackageRequiresNonTestGoSource(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{name: "empty package"},
		{name: "test files only", files: map[string]string{"library_test.go": "package loose\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := copyLoosePackage(require.Requirement{LocalPath: root}, filepath.Join(t.TempDir(), "copied"))
			if err == nil || !strings.Contains(err.Error(), "contains no non-test Go source files") {
				t.Fatalf("copyLoosePackage error = %v; want no-source error", err)
			}
		})
	}
}

func TestCopyLoosePackageCopiesGoSource(t *testing.T) {
	root := t.TempDir()
	const source = "package loose\n"
	if err := os.WriteFile(filepath.Join(root, "library.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copied")
	if err := copyLoosePackage(require.Requirement{LocalPath: root}, destination); err != nil {
		t.Fatalf("copyLoosePackage: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(destination, "library.go"))
	if err != nil {
		t.Fatalf("read copied source: %v", err)
	}
	if string(copied) != source {
		t.Fatalf("copied source = %q, want %q", copied, source)
	}
}

func TestIsPackageMainParsesPackageClauseAroundComments(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{name: "plain main", src: "package main\n", want: true},
		{name: "trailing comment", src: "package main // comment\n", want: true},
		{name: "line comment contains fake clause", src: "// package main\npackage library\n", want: false},
		{name: "multiline block comment contains fake clause", src: "/*\npackage main\n*/\npackage library\n", want: false},
		{name: "block comment before real clause", src: "/* header */ package main\n", want: true},
		{name: "non-main package", src: "package library\n", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPackageMain([]byte(tc.src)); got != tc.want {
				t.Fatalf("isPackageMain(%q) = %t, want %t", tc.src, got, tc.want)
			}
		})
	}
}
