package core_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestStripExprComments_DepthAware(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no hash — passthrough",
			in:   `a + b`,
			want: `a + b`,
		},
		{
			name: "trailing comment at depth 0",
			in:   `x > 0  # positive`,
			want: `x > 0  `,
		},
		{
			name: "iterator at depth 1 survives",
			in:   `filter(# > 0)`,
			want: `filter(# > 0)`,
		},
		{
			name: "iterator and trailing comment coexist",
			in:   `all(tags, # != "beta")  # no beta`,
			want: `all(tags, # != "beta")  `,
		},
		{
			name: "comment inside string is preserved",
			in:   `name == "#not-a-comment"`,
			want: `name == "#not-a-comment"`,
		},
		{
			name: "comment inside backtick string is preserved",
			in:   "x == `# raw`",
			want: "x == `# raw`",
		},
		{
			name: "multiline preserves line terminators",
			in:   "a\n# comment\nb",
			want: "a\n\nb",
		},
		{
			name: "CRLF preserved",
			in:   "a  # note\r\nb",
			want: "a  \r\nb",
		},
		{
			name: "nested parens — depth returns to 0",
			in:   `f(g(x))  # done`,
			want: `f(g(x))  `,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, core.StripExprComments(tc.in), tc.want)
		})
	}
}
