package fs

import (
	"bytes"
	"context"
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestReadLinesTrimsBlankLinesAndComments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "repos.txt")
	content := "  github.com/nexssp/kernel  # core\n\n# whole-line comment\nhttps://github.com/nexssp/repo\r\ngithub.com/nexssp/flow"
	ktest.RequireNoError(t, os.WriteFile(path, []byte(content), 0o600))

	got, err := ReadLinesAction.DoAny(context.Background(), map[string]any{"path": path, "comment_prefix": "#"})
	ktest.RequireNoError(t, err)
	lines, ok := got.([]string)
	ktest.RequireCondition(t, ok, "ReadLinesAction result type = %T, want []string", got)
	ktest.RequireEqual(t, lines, []string{"github.com/nexssp/kernel", "https://github.com/nexssp/repo", "github.com/nexssp/flow"})
}

func TestReadLinesWithoutCommentPrefixKeepsHashText(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "lines.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("# literal\n value \n"), 0o600))

	got, err := ReadLinesAction.DoAny(context.Background(), map[string]any{"path": path})
	ktest.RequireNoError(t, err)
	lines, ok := got.([]string)
	ktest.RequireCondition(t, ok, "ReadLinesAction result type = %T, want []string", got)
	ktest.RequireEqual(t, lines, []string{"# literal", "value"})
}

func TestReadLinesEmptyFileReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.txt")
	ktest.RequireNoError(t, os.WriteFile(path, nil, 0o600))

	got, err := ReadLinesAction.DoAny(context.Background(), map[string]any{"path": path})
	ktest.RequireNoError(t, err)
	lines, ok := got.([]string)
	ktest.RequireCondition(t, ok, "ReadLinesAction result type = %T, want []string", got)
	ktest.RequireEqual(t, lines, []string{})
}

func TestReadLinesClassifiesBadPathsAndReadFailures(t *testing.T) {
	tests := []struct {
		name string
		path string
		kind xerr.Kind
	}{
		{name: "empty path", kind: xerr.KindBadRequest},
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing.txt"), kind: xerr.KindNotFound},
		{name: "directory is not a text file", path: t.TempDir(), kind: xerr.KindInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadLinesAction.DoAny(context.Background(), map[string]any{"path": test.path})
			ktest.RequireNotNil(t, err)
			ktest.RequireEqual(t, xerr.KindFrom(err), test.kind)
		})
	}
}

func TestReadLinesReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ReadLinesAction.DoAny(ctx, map[string]any{"path": filepath.Join(t.TempDir(), "lines.txt")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadLinesAction error = %v, want context.Canceled", err)
	}
}

func TestReadLinesAllowsLineLargerThanScannerDefault(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "long.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 70*1024)), 0o600))

	got, err := ReadLinesAction.DoAny(context.Background(), map[string]any{"path": path})
	ktest.RequireNoError(t, err)
	lines, ok := got.([]string)
	ktest.RequireCondition(t, ok, "ReadLinesAction result type = %T, want []string", got)
	ktest.RequireEqual(t, len(lines), 1)
	ktest.RequireEqual(t, len(lines[0]), 70*1024)
}

func TestReadLinesAllowsLineAtSupportedLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limit.txt")
	line := strings.Repeat("x", maxTextLineBytes)
	ktest.RequireNoError(t, os.WriteFile(path, []byte(line+"\r\n"), 0o600))

	got, err := ReadLinesAction.DoAny(context.Background(), map[string]any{"path": path})
	ktest.RequireNoError(t, err)
	lines, ok := got.([]string)
	ktest.RequireCondition(t, ok, "ReadLinesAction result type = %T, want []string", got)
	ktest.RequireEqual(t, len(lines), 1)
	ktest.RequireEqual(t, len(lines[0]), maxTextLineBytes)
}

func TestReadLinesRejectsLineAboveSupportedLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "too-long.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte(strings.Repeat("x", maxTextLineBytes+1)), 0o600))

	_, err := ReadLinesAction.DoAny(context.Background(), map[string]any{"path": path})
	ktest.RequireNotNil(t, err)
	ktest.RequireEqual(t, xerr.KindFrom(err), xerr.KindInternal)
}

func TestReadLinesFixture(t *testing.T) {
	source, err := stdfs.ReadFile(Bundle(nil).Fixtures, "nflows/read_lines.nflow")
	ktest.RequireNoError(t, err)

	workingDir := t.TempDir()
	path := filepath.Join(workingDir, "repositories.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("  github.com/nexssp/kernel # core\n\n# skipped\ngithub.com/nexssp/flow\n"), 0o600))
	t.Chdir(workingDir)

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		syntax.Bundle(nil),
		description.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, string(source), "read_lines.nflow", map[string]any{}, runner.Opts{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("RunSource returned %d; stdout=%q; stderr=%q", code, stdout.String(), stderr.String())
	}
	ktest.RequireEqual(t, stderr.String(), "")
}
