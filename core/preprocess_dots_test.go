package core_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
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
		{"bare dot", `.`, `__root__`},
		{"member access", `user.name`, `user.name`},
		{"member after call", `fn().name`, `fn().name`},
		{"member after index", `arr[0].x`, `arr[0].x`},
		{"member after iterator", `#.name`, `#.name`},
		{"inside dq string", `"a.b"`, `"a.b"`},
		{"inside sq string", `'a.b'`, `'a.b'`},
		{"inside backtick", "`a.b`", "`a.b`"},
		{"escaped quote in string", `"a\"b.c"`, `"a\"b.c"`},
		{"dot before digit is root", `.123`, `__root__123`},    // regression
		{"dot before close-paren is member", `foo.)`, `foo.)`}, // regression
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, core.PreprocessDots(tc.in), tc.want)
		})
	}
}
