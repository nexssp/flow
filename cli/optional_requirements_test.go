package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestSourceRequiresDoesNotInferSelftestkitFromText(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"# cov.echo\nruntime.noop\n",
		"{value: \"cov.echo\"} -> runtime.noop\n",
		"cov.echo\n",
	} {
		path := filepath.Join(t.TempDir(), "main.nflow")
		ktest.RequireNoError(t, os.WriteFile(path, []byte(source), 0o600))
		reqs, err := SourceRequires(path)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, len(reqs), 0)
	}
}

func TestSourceRequiresKeepsExplicitSelftestkitInInclude(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "main.nflow")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("@include child.nflow\n"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(root, "child.nflow"), []byte("@require selftestkit\ncov.echo\n"), 0o600))
	reqs, err := SourceRequires(path)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(reqs), 1)
	ktest.RequireEqual(t, reqs[0].Import, "selftestkit")
}
