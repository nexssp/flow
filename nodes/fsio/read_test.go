package fsio_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow/nodes/fsio"
	"github.com/nexssp/kernel/action"
)

func TestRead_StreamWithBufferPool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	testData := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filePath, []byte(testData), 0o600); err != nil {
		t.Fatal(err)
	}

	readOp, err := fsio.ReadOperator().Build(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	src := action.StreamFromSlice([]fsio.FileMeta{{
		Path:    filePath,
		RelPath: "sample.txt",
	}})

	// Wrap the typed iterator as AnyStream (the untyped operator boundary).
	anyStream, err := readOp.Apply(action.AnyStream(func(yield func(any, error) bool) {
		for item, itemErr := range src {
			if !yield(item, itemErr) {
				return
			}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}

	var readItems []fsio.FileContent
	anyStream(func(item any, itemErr error) bool {
		if itemErr != nil {
			t.Fatal(itemErr)
		}
		readItems = append(readItems, item.(fsio.FileContent))
		return true
	})

	if len(readItems) != 1 {
		t.Fatalf("expected 1 file read, got %d", len(readItems))
	}
	if string(readItems[0].Content) != testData {
		t.Fatalf("content mismatch: got %q", string(readItems[0].Content))
	}
}

func TestRead_LineRangeFiltering(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "numbers.txt")
	testData := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filePath, []byte(testData), 0o600); err != nil {
		t.Fatal(err)
	}

	readOp, err := fsio.ReadOperator().Build(map[string]any{"lines": "2-4"})
	if err != nil {
		t.Fatal(err)
	}

	src := action.StreamFromSlice([]fsio.FileMeta{{
		Path:    filePath,
		RelPath: "numbers.txt",
	}})

	anyStream, err := readOp.Apply(action.AnyStream(func(yield func(any, error) bool) {
		for item, itemErr := range src {
			if !yield(item, itemErr) {
				return
			}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}

	var content string
	anyStream(func(item any, itemErr error) bool {
		if itemErr != nil {
			t.Fatal(itemErr)
		}
		content = string(item.(fsio.FileContent).Content)
		return true
	})

	want := "line 2\nline 3\nline 4\n"
	if content != want {
		t.Fatalf("line filtering mismatch:\ngot:  %q\nwant: %q", content, want)
	}
}
