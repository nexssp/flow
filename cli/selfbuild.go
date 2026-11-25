package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nexssp/flow/builder"
)

// SelfBuild rebuilds the running nflow binary in place, injecting
// VCS metadata from the source directory. It replaces the manual
// `go install` step for developers who want consistent version output.
func SelfBuild(ctx context.Context) error {
	dir, err := builder.FlowModuleRoot()
	if err != nil {
		return err
	}

	output := goInstallTarget(ctx, "nflow")

	version := builder.GitDescribe(ctx, dir)
	commit := builder.GitCommit(ctx, dir)
	builtAt := time.Now().UTC().Format(time.RFC3339)

	ldflags := selfBuildLinkerFlags(version, commit, builtAt)

	fmt.Fprintf(os.Stderr, "building nflow\n")
	fmt.Fprintf(os.Stderr, "  source:   %s\n", dir)
	fmt.Fprintf(os.Stderr, "  output:   %s\n", output)
	fmt.Fprintf(os.Stderr, "  version:  %s\n", version)
	fmt.Fprintf(os.Stderr, "  commit:   %s\n", commit)

	//nolint:gosec // G204: output path and ldflags are derived from the local module root.
	cmd := exec.CommandContext(ctx, "go", "build", "-ldflags="+ldflags, "-o", output, "./cmd/nflow")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("self-build: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ installed %s\n", output)
	return nil
}

func selfBuildLinkerFlags(version, commit, builtAt string) string {
	return fmt.Sprintf(
		"-s -w -X github.com/nexssp/flow/cli.Version=%s -X github.com/nexssp/flow/cli.Commit=%s -X github.com/nexssp/flow/cli.BuiltAt=%s -X github.com/nexssp/flow/builder.Version=%s -X github.com/nexssp/flow/builder.Commit=%s -X github.com/nexssp/flow/builder.BuiltAt=%s",
		version, commit, builtAt, version, commit, builtAt,
	)
}

// goInstallTarget returns the path Go uses for `go install` on the
// current platform: $GOBIN, or $GOPATH/bin, with .exe on Windows.
func goInstallTarget(ctx context.Context, name string) string {
	bin := os.Getenv("GOBIN")
	if bin == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			out, err := exec.CommandContext(ctx, "go", "env", "GOPATH").Output()
			if err == nil {
				gopath = strings.TrimSpace(string(out))
			}
		}
		if gopath != "" {
			bin = filepath.Join(gopath, "bin")
		}
	}
	if bin == "" {
		bin = "."
	}
	if runtime.GOOS == windowsOS {
		name += ".exe"
	}
	return filepath.Join(bin, name)
}
