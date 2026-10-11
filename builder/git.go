package builder

import (
	"context"
	"os/exec"
	"strings"
)

// GitDescribe returns the closest tag or revision for a source directory.
func GitDescribe(ctx context.Context, dir string) string { return gitDescribe(ctx, dir) }

// GitCommit returns the short revision for a source directory.
func GitCommit(ctx context.Context, dir string) string { return gitCommit(ctx, dir) }

func gitDescribe(ctx context.Context, dir string) string {
	if out, err := runGit(ctx, dir, "describe", "--tags", "--always", "--dirty"); err == nil {
		return strings.TrimSpace(out)
	}
	if out, err := runGit(ctx, dir, "rev-parse", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	return "dev"
}

func gitCommit(ctx context.Context, dir string) string {
	out, err := runGit(ctx, dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(out)
	if dirty, err := runGit(ctx, dir, "status", "--porcelain"); err == nil && strings.TrimSpace(dirty) != "" {
		commit += "-dirty"
	}
	return commit
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
