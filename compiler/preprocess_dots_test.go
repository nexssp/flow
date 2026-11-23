package compiler_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestPreprocessDots_UnifiedBehavior(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"leading dot", `.name`, `name`},
		{"nested access", `.user.name`, `user.name`},
		{"input field path", `.input.name`, `__root__.input.name`},
		{"bare dot", `.`, `__root__`},
		{"member access", `user.name`, `user.name`},
		{"member after call", `fn().name`, `fn().name`},
		{"member after index", `arr[0].x`, `arr[0].x`},
		{"member after iterator", `#.name`, `#.name`},
		{"inside dq string", `"a.b"`, `"a.b"`},
		{"inside sq string", `'a.b'`, `'a.b'`},
		{"inside backtick", "`a.b`", "`a.b`"},
		{"backslash does not escape raw closing tick", "`raw\\` + .value", "`raw\\` + value"},
		{"escaped quote in string", `"a\"b.c"`, `"a\"b.c"`},
		{"dot before digit is root", `.123`, `__root__123`},    // regression
		{"dot before close-paren is member", `foo.)`, `foo.)`}, // regression
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, compiler.PreprocessDots(tc.in), tc.want)
		})
	}
}

func TestPreprocessDotsWithTypedPipelineRoots(t *testing.T) {
	t.Parallel()
	roots := map[string]struct{}{"request": {}}
	for _, tc := range []struct {
		source string
		want   string
	}{
		{source: `.request.task`, want: `__root__.request.task`},
		{source: `request.task`, want: `request.task`},
		{source: `other.request.task`, want: `other.request.task`},
		{source: `".request.task"`, want: `".request.task"`},
	} {
		if got := compiler.PreprocessDotsWithRoots(tc.source, roots); got != tc.want {
			t.Errorf("PreprocessDotsWithRoots(%q) = %q, want %q", tc.source, got, tc.want)
		}
	}
}
