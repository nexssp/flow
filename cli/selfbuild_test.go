package cli

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSelfBuildUsesNFlowInstallTarget(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)

	want := filepath.Join(binDir, "nflow")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := goInstallTarget(context.Background(), "nflow"); got != want {
		t.Fatalf("goInstallTarget() = %q, want %q", got, want)
	}
}

func TestSelfBuildLinkerFlagsUseModuleImportPaths(t *testing.T) {
	const (
		version = "v1.0.0"
		commit  = "abc123"
		builtAt = "2026-11-25T00:00:00Z"
	)
	want := "-s -w -X github.com/nexssp/flow/cli.Version=v1.0.0 -X github.com/nexssp/flow/cli.Commit=abc123 -X github.com/nexssp/flow/cli.BuiltAt=2026-11-25T00:00:00Z -X github.com/nexssp/flow/builder.Version=v1.0.0 -X github.com/nexssp/flow/builder.Commit=abc123 -X github.com/nexssp/flow/builder.BuiltAt=2026-11-25T00:00:00Z"
	if got := selfBuildLinkerFlags(version, commit, builtAt); got != want {
		t.Fatalf("selfBuildLinkerFlags() = %q, want %q", got, want)
	}
}
