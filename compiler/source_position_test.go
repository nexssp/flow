package compiler

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestDirectiveReqPositionAt(t *testing.T) {
	t.Parallel()
	req := DirectiveReq{Lines: []string{"alpha", "beta"}, File: "flow.nflow", I: 1}
	for _, tc := range []struct {
		name   string
		offset int
		want   Position
	}{
		{name: "first line start", offset: 0, want: Position{File: "flow.nflow", Line: 1, Col: 1}},
		{name: "first line end", offset: 5, want: Position{File: "flow.nflow", Line: 1, Col: 6}},
		{name: "second line start", offset: 6, want: Position{File: "flow.nflow", Line: 2, Col: 1}},
		{name: "second line middle", offset: 8, want: Position{File: "flow.nflow", Line: 2, Col: 3}},
		{name: "out of range fallback", offset: 20, want: Position{File: "flow.nflow", Line: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, req.PositionAt(tc.offset), tc.want)
		})
	}
}

func TestDirectiveReqOffsetFallback(t *testing.T) {
	t.Parallel()
	req := DirectiveReq{Lines: []string{"alpha", "beta"}}
	for _, tc := range []struct {
		name   string
		line   int
		column int
		want   int
	}{
		{name: "line start", line: 1, want: 6},
		{name: "clamped column", line: 1, column: 10, want: 10},
		{name: "end of source", line: 2, want: 10},
		{name: "line before source", line: -1},
		{name: "line after source", line: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, req.Offset(tc.line, tc.column), tc.want)
		})
	}
}
