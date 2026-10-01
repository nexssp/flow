package require

import "testing"

func TestNormalizeID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in  string
		out string
	}{
		{"macros", "macros"},
		{"github.com/nexssp/flow/extensions/macros", "macros"},
		{"github.com/nexssp/mybundle", "mybundle"},
		{"github.com/nexssp/mybundle/nexssflow", "mybundle"},
		{"github.com/nexssp/tools/nexssflow/", "tools"},
		{"nexssflow", "nexssflow"},
		{"", ""},
		{"/", ""},
		{"//", ""},
		{`.\custom\nexssflow`, "custom"},
		{`..\..\shared\ai`, "ai"},
		{`C:\dev\repo\nexssflow`, "repo"},
	}

	for _, c := range cases {
		got := NormalizeID(c.in)
		if got != c.out {
			t.Errorf("NormalizeID(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}
