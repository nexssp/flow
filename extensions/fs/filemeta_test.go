package fs

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestDetectLang(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a.go":         "go",
		"b.ts":         "typescript",
		"c.tsx":        "typescript",
		"d.js":         "javascript",
		"e.py":         "python",
		"f.rs":         "rust",
		"g.md":         "markdown",
		"h.yml":        "yaml",
		"i.json":       "json",
		"j.toml":       "toml",
		"unknown.xyz":  "",
		"uppercase.GO": "go",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, detectLang(path), want)
		})
	}
}

func TestParseExtSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want map[string]bool
	}{
		{"empty", "", nil},
		{"single", "go", map[string]bool{"go": true}},
		{"list", "go,ts,py", map[string]bool{"go": true, "ts": true, "py": true}},
		{"with dots", ".go,.ts", map[string]bool{"go": true, "ts": true}},
		{"with spaces", " go , ts ", map[string]bool{"go": true, "ts": true}},
		{"uppercase", "GO,TS", map[string]bool{"go": true, "ts": true}},
		{"empty entries", "go,,ts,", map[string]bool{"go": true, "ts": true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, parseExtSet(c.in), c.want)
		})
	}
}
