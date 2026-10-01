package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestParseLineRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		spec    string
		want    []lineRange
		wantErr bool
	}{
		{"empty", "", nil, false},
		{"single", "10", []lineRange{{start: 10, end: 10}}, false},
		{"range", "10-20", []lineRange{{start: 10, end: 20}}, false},
		{"open range", "10-", []lineRange{{start: 10, end: -1}}, false},
		{"union", "1,3,5-7", []lineRange{
			{start: 1, end: 1},
			{start: 3, end: 3},
			{start: 5, end: 7},
		}, false},
		{"invalid number", "abc", nil, true},
		{"negative", "-5", nil, true},
		{"bad range", "5-3", nil, true},
		{"zero", "0", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseLineRanges(c.spec)
			if c.wantErr {
				ktest.RequireCondition(t, err != nil, "expected error for %q", c.spec)
				return
			}
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestIsLineIncluded(t *testing.T) {
	t.Parallel()
	ranges := []lineRange{{start: 5, end: 10}, {start: 20, end: -1}}
	cases := map[int]bool{
		4: false, 5: true, 7: true, 10: true, 11: false,
		19: false, 20: true, 100: true,
	}
	for line, want := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, isLineIncluded(line, ranges), want)
		})
	}
}

func TestRead_WholeFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o600))

	scratch := make([]byte, readBufferSize)
	got, err := readFileContent(path, nil, scratch)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(got), "line1\nline2\nline3\n")
}

func TestRead_LineRanges(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("l1\nl2\nl3\nl4\nl5\n"), 0o600))

	ranges, _ := parseLineRanges("2-4")
	got, err := readFileContent(path, ranges, nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(got), "l2\nl3\nl4\n")
}

func TestRead_OpenRange(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("l1\nl2\nl3\n"), 0o600))

	ranges, _ := parseLineRanges("2-")
	got, err := readFileContent(path, ranges, nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(got), "l2\nl3\n")
}
