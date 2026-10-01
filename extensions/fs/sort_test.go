package fs

import (
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestSort_ByRelPath(t *testing.T) {
	t.Parallel()
	in := []FileMeta{
		{RelPath: "c"},
		{RelPath: "a"},
		{RelPath: "b"},
	}
	got := collectSort(t, SortConfig{By: "rel_path"}, in)
	ktest.RequireEqual(t, []string{got[0].RelPath, got[1].RelPath, got[2].RelPath}, []string{"a", "b", "c"})
}

func TestSort_BySize(t *testing.T) {
	t.Parallel()
	in := []FileMeta{
		{RelPath: "big", Size: 100},
		{RelPath: "small", Size: 1},
		{RelPath: "mid", Size: 50},
	}
	got := collectSort(t, SortConfig{By: "size"}, in)
	ktest.RequireEqual(t, []int64{got[0].Size, got[1].Size, got[2].Size}, []int64{1, 50, 100})
}

func TestSort_ByModTime(t *testing.T) {
	t.Parallel()
	base := time.Now()
	in := []FileMeta{
		{RelPath: "new", ModTime: base.Add(time.Hour)},
		{RelPath: "old", ModTime: base},
		{RelPath: "mid", ModTime: base.Add(time.Minute)},
	}
	got := collectSort(t, SortConfig{By: "mod_time"}, in)
	ktest.RequireEqual(t, []string{got[0].RelPath, got[1].RelPath, got[2].RelPath}, []string{"old", "mid", "new"})
}

func TestSort_MaxItemsExceeded(t *testing.T) {
	t.Parallel()
	in := []FileMeta{{RelPath: "a"}, {RelPath: "b"}, {RelPath: "c"}}
	stream := Sort(SortConfig{MaxItems: 2})(sliceSeq(in))

	var sawError bool
	for _, err := range stream {
		if err != nil {
			sawError = true
		}
	}
	ktest.RequireCondition(t, sawError, "expected max_items error")
}

func collectSort(tb testing.TB, cfg SortConfig, in []FileMeta) []FileMeta {
	tb.Helper()
	var out []FileMeta
	for meta, err := range Sort(cfg)(sliceSeq(in)) {
		ktest.RequireNoError(tb, err)
		out = append(out, meta)
	}
	return out
}
