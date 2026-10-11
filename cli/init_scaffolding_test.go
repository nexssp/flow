package cli

import (
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
