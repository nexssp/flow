package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/xerr"
)

const (
	cacheRoot     = ".nexss"
	goBuildCached = true
)

// ResolveRequires returns the path to a runnable binary that provides
// the required libraries. When reqs is empty, it returns the current
// executable so the caller can re-exec itself with no codegen.
//
// When reqs is non-empty, the harness is generated under
// <cwd>/.nexss/cache/<key>/ and built once. Subsequent calls with the
// same requirements reuse the cached binary.
func ResolveRequires(
	ctx context.Context,
	reqs []flow.Requirement,
	stdout, stderr *os.File,
) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", xerr.Internal("nexss: locate self", err)
	}

	if len(reqs) == 0 {
		return exe, nil
	}

	key, err := requiresCacheKey(reqs, exe)
	if err != nil {
		return "", err
	}

	dir := filepath.Join(cacheRoot, "cache", key)

	binary := filepath.Join(dir, "runner")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	if _, err := os.Stat(binary); err == nil {
		return binary, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", xerr.Internal("nexss: mkdir cache dir", err)
	}

	fmt.Fprintf(stdout, "⚙ codegen: building flow runner harness for %d requirement(s)\n", len(reqs))

	for _, r := range reqs {
		fmt.Fprintf(stdout, "  ├── %s %s\n", r.Import, r.Version)
	}

	flowModule := locateFlowModule()
	if flowModule == "" {
		fmt.Fprintf(stdout, "⚠️  could not locate local nexssp workspace; go build will use network modules\n")
	}

	if err := writeHarness(dir, reqs, flowModule); err != nil {
		return "", err
	}

	if err := runGo(ctx, dir, stdout, stderr, "mod", "tidy"); err != nil {
		return "", err
	}

	for _, r := range reqs {
		if r.Version == "" {
			continue
		}

		target := r.Import + "@" + r.Version
		if err := runGo(ctx, dir, stdout, stderr, "get", target); err != nil {
			return "", xerr.Internal("nexss: go get "+target, err)
		}
	}

	if err := runGo(ctx, dir, stdout, stderr, "mod", "tidy"); err != nil {
		return "", err
	}

	fmt.Fprintf(stdout, "⚙ codegen: go build …\n")

	if err := runGo(ctx, dir, stdout, stderr, "build", "-o", binary, "."); err != nil {
		return "", err
	}

	fmt.Fprintf(stdout, "✅ codegen: runner ready at %s\n", binary)

	return binary, nil
}

func runGo(ctx context.Context, dir string, stdout, stderr *os.File, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Stdout = stdout

	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return xerr.Internal("nexss: go "+strings.Join(args, " "), err)
	}

	return nil
}

func requiresCacheKey(reqs []flow.Requirement, runnerExe string) (string, error) {
	sorted := append([]flow.Requirement(nil), reqs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Import != sorted[j].Import {
			return sorted[i].Import < sorted[j].Import
		}

		return sorted[i].Version < sorted[j].Version
	})

	var b strings.Builder
	for _, r := range sorted {
		b.WriteString(r.Import)
		b.WriteByte('@')
		b.WriteString(r.Version)
		b.WriteByte('\n')
	}

	if info, err := os.Stat(runnerExe); err == nil {
		fmt.Fprintf(&b, "runner:%d\n", info.ModTime().UnixNano())
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:8]), nil
}

// locateFlowModule walks up from the current working directory looking
// for a nexssp workspace: a directory containing flow/library.go and a
// sibling go.mod. Returns the absolute path to flow/, or "" when none
// is found.
func locateFlowModule() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "flow", "library.go")); err == nil {
			return filepath.Join(dir, "flow")
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
}
