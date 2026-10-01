package require

import (
	"testing"

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
}
