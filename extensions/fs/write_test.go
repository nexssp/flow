package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestWriteCreatesNestedFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "output")
	fileMeta := FileMeta{RelPath: filepath.Join("nested", "result.txt")}
	writeFile := FileContent{FileMeta: fileMeta, Content: []byte("written")}

	for _, err := range Write(WriteConfig{Dir: root})(sliceSeq([]FileContent{writeFile})) {
		ktest.RequireNoError(t, err)
	}

	got, err := os.ReadFile(filepath.Join(root, "nested", "result.txt"))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(got), "written")
}

func TestWriteDoesNotOverwriteWithoutFlag(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "result.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("original"), 0o600))

	err := writeTestFile(root, "result.txt", false, "replacement")
	ktest.RequireErrorContains(t, err, "fs.write")

	got, err := os.ReadFile(path)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(got), "original")
}

func TestWriteRejectsSymlinkedParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	err := writeTestFile(root, filepath.Join("escape", "pwned.txt"), true, "outside")
	ktest.RequireErrorContains(t, err, "fs.write")

	_, err = os.Stat(filepath.Join(outside, "pwned.txt"))
	ktest.RequireCondition(t, errors.Is(err, os.ErrNotExist), "symlink target was written: %v", err)
}

func TestWriteRejectsSymlinkedFileEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	ktest.RequireNoError(t, os.WriteFile(outside, []byte("original"), 0o600))
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	err := writeTestFile(root, "link.txt", true, "replacement")
	ktest.RequireErrorContains(t, err, "fs.write")

	got, err := os.ReadFile(outside)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(got), "original")
}

func writeTestFile(root, relativePath string, overwrite bool, content string) error {
	fileMeta := FileMeta{RelPath: relativePath}
	item := FileContent{FileMeta: fileMeta, Content: []byte(content)}
	for _, err := range Write(WriteConfig{Dir: root, Overwrite: overwrite})(sliceSeq([]FileContent{item})) {
		if err != nil {
			return err
		}
	}
	return nil
}
