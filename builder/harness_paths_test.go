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
