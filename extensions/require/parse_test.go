package require

import (
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestSplitOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{"single", `id: "custom"`, map[string]string{"id": "custom"}, false},
		{"two", `id: "x", cmd: "y"`, map[string]string{"id": "x", "cmd": "y"}, false},
		{"newline separated", "id: \"x\"\ncmd: \"y\"", map[string]string{"id": "x", "cmd": "y"}, false},
		{"quoted newline", "text: \"first\nsecond\"\nid: \"x\"", map[string]string{"text": "first\nsecond", "id": "x"}, false},
		{"empty", ``, map[string]string{}, false},
		{"missing colon", `nope`, nil, true},
		{"empty key", `: "x"`, nil, true},
		{"single quote", `id: 'x'`, map[string]string{"id": "x"}, false},
		{"backtick", "id: `x`", map[string]string{"id": "x"}, false},
		{"unquoted value", `id: x`, map[string]string{"id": "x"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := splitOptions(c.raw)
			if c.wantErr {
				ktest.RequireCondition(t, err != nil, "expected error for %q", c.raw)
				if xerr.KindFrom(err) != xerr.KindValidation {
					t.Fatalf("error for %q = %v, want Validation", c.raw, err)
				}
				return
			}
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestParseInlineOptions_SingleLine(t *testing.T) {
	t.Parallel()
	got, next, err := parseInlineOptions(`{ id: "x" }`, nil, 0)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got["id"], "x")
	ktest.RequireEqual(t, next, 1)
}

func TestParseRemoteSelectors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		selector string
		want     string
	}{
		{selector: "1.0.0", want: "1.0.0"},
		{selector: "v1.2.3", want: "v1.2.3"},
		{selector: "release/v2", want: "release/v2"},
		{selector: "feature-branch", want: "feature-branch"},
		{selector: "0123456789abcdef0123456789abcdef01234567", want: "0123456789abcdef0123456789abcdef01234567"},
		{selector: "latest", want: "latest"},
		{selector: `"release/v2"`, want: "release/v2"},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			t.Parallel()
			got, err := Parse("example.com/foo "+tc.selector, "", nil, "test.nflow", 1)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got.Version, tc.want)
		})
	}
}

func TestParseRejectsInvalidRemoteSelectors(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{
		"-branch",
		"branch@revision",
		`""`,
		`" "`,
		`"branch`,
		"bad:selector",
		"bad\x01selector",
		"branch extra",
	} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			_, err := Parse("example.com/foo "+selector, "", nil, "test.nflow", 1)
			ktest.RequireCondition(t, err != nil, "expected invalid selector %q to be rejected", selector)
		})
	}
}

func TestParseInlineOptions_MultiLine(t *testing.T) {
	t.Parallel()
	lines := []string{
		`@require foo {`,
		`  id: "x"`,
		`}`,
	}
	got, next, err := parseInlineOptions(`{`, lines, 0)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got["id"], "x")
	ktest.RequireEqual(t, next, 3)
}

func TestParseInlineOptions_Unclosed(t *testing.T) {
	t.Parallel()
	lines := []string{`@require foo {`, `  id: "x"`}
	_, _, err := parseInlineOptions(`{`, lines, 0)
	ktest.RequireErrorContains(t, err, "unclosed")
	if xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("unclosed inline options error = %v, want Validation", err)
	}
}

func TestParseBlockOptions(t *testing.T) {
	t.Parallel()
	lines := []string{`{`, `  id: "x"`, `}`}
	got, next, err := parseBlockOptions(lines, 0)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got["id"], "x")
	ktest.RequireEqual(t, next, 3)
}

func TestParseBlockOptions_Unclosed(t *testing.T) {
	t.Parallel()
	lines := []string{`{`, `  id: "x"`}
	_, _, err := parseBlockOptions(lines, 0)
	ktest.RequireErrorContains(t, err, "unclosed")
	if xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("unclosed block options error = %v, want Validation", err)
	}
}
