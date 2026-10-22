package cli

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestInit_ProjectScaffolding(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	if code := runInit([]string{}); code != 0 {
		t.Fatalf("runInit failed with code %d", code)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "workflow.nflow")); err != nil {
		t.Fatalf("expected workflow.nflow to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "helpers", "library.go")); err != nil {
		t.Fatalf("expected helpers/library.go to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "README.md")); err != nil {
		t.Fatalf("expected README.md to exist: %v", err)
	}
}

func TestInit_BundleScaffolding(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	if code := runInit([]string{"--bundle"}); code != 0 {
		t.Fatalf("runInit --bundle failed with code %d", code)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "nexssflow", "library.go")); err != nil {
		t.Fatalf("expected nexssflow/library.go: %v", err)
	}

	if code := runInit([]string{"--bundle", "nexssflow_dev"}); code != 0 {
		t.Fatalf("runInit --bundle nexssflow_dev failed with code %d", code)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "nexssflow_dev", "library.go")); err != nil {
		t.Fatalf("expected nexssflow_dev/library.go: %v", err)
	}
}

func TestInit_BundleRejectsInvalidPackageIdentifier(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	if code := runInit([]string{"--bundle", "bad-package"}); code == 0 {
		t.Fatal("runInit accepted a directory name that is not a Go package identifier")
	}
	if _, err := os.Stat(filepath.Join(tempDir, "bad-package")); !os.IsNotExist(err) {
		t.Fatalf("invalid package directory was created: %v", err)
	}
}

func TestInit_BundleQuotesActionIDFromWorkingDirectory(t *testing.T) {
	parent := t.TempDir()
	workingDir := filepath.Join(parent, `project"; func injected() {}`)
	if err := os.Mkdir(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workingDir)

	if code := runInit([]string{"--bundle"}); code != 0 {
		t.Fatalf("runInit --bundle failed with code %d", code)
	}
	source, err := os.ReadFile(filepath.Join(workingDir, "nexssflow", "library.go"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "library.go", source, parser.AllErrors); err != nil {
		t.Fatalf("scaffold generated invalid Go: %v\n%s", err, source)
	}
}
