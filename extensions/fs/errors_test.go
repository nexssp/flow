package fs

import (
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestFSRead_InvalidLinesIsValidation(t *testing.T) {
	t.Parallel()

	source := WalkSource()
	_ = source

	op := Read(ReadConfig{Lines: "abc"})
	stream := op(sliceSeq([]FileMeta{{Path: "/tmp/whatever", RelPath: "whatever"}}))

	var got error
	for _, err := range stream {
		if err != nil {
			got = err
			break
		}
	}
	ktest.RequireNotNil(t, got)
	ktest.RequireEqual(t, xerr.KindFrom(got), xerr.KindValidation)
}

func TestFSSort_MaxItemsExceededIsValidation(t *testing.T) {
	t.Parallel()

	in := []FileMeta{{RelPath: "a"}, {RelPath: "b"}, {RelPath: "c"}}
	stream := Sort(SortConfig{MaxItems: 2})(sliceSeq(in))

	var got error
	for _, err := range stream {
		if err != nil {
			got = err
		}
	}
	ktest.RequireNotNil(t, got)
	ktest.RequireEqual(t, xerr.KindFrom(got), xerr.KindValidation)
}
