package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runLoad(tb testing.TB, baseDir, line string) (map[string]string, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleConfigLoad(context.Background(), core.DirectiveReq{
		Lines:   []string{line},
		I:       0,
		Out:     out,
		File:    "<test>",
		BaseDir: baseDir,
	})
	cfg, _ := out["config"].(map[string]string)
	return cfg, err
}

func writeFixture(tb testing.TB, name, content string) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), name)
	ktest.RequireNoError(tb, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoad_YAML(t *testing.T) {
	path := writeFixture(t, "nexss.yml", "host: api.example.test\nport: 8443\n")
	cfg, err := runLoad(t, "", `@config.load:path="`+path+`"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["host"], "api.example.test")
	ktest.RequireEqual(t, cfg["port"], "8443")
}

func TestLoad_JSON(t *testing.T) {
	path := writeFixture(t, "nexss.json", `{"host":"x","nested":{"deep":"y"}}`)
	cfg, err := runLoad(t, "", `@config.load:path="`+path+`"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["host"], "x")
	ktest.RequireEqual(t, cfg["nested.deep"], "y")
}

func TestLoad_TOML(t *testing.T) {
	path := writeFixture(t, "nexss.toml", "[db]\nhost = \"localhost\"\nport = 5432\n")
	cfg, err := runLoad(t, "", `@config.load:path="`+path+`"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["db.host"], "localhost")
	ktest.RequireEqual(t, cfg["db.port"], "5432")
}

func TestLoad_Prefix(t *testing.T) {
	path := writeFixture(t, "a.yml", "x: 1\n")
	cfg, err := runLoad(t, "", `@config.load:path="`+path+`" :prefix="app"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["app.x"], "1")
}

func TestLoad_MissingOptional(t *testing.T) {
	cfg, err := runLoad(t, "", `@config.load:path="/nonexistent/x.yml" :required=false`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(cfg), 0)
}

func TestLoad_MissingRequired(t *testing.T) {
	_, err := runLoad(t, "", `@config.load:path="/nonexistent/x.yml"`)
	ktest.RequireErrorContains(t, err, "read")
}

func TestLoad_RelativeToBaseDir(t *testing.T) {
	dir := t.TempDir()
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(dir, "c.yml"), []byte("k: v\n"), 0o600))
	cfg, err := runLoad(t, dir, `@config.load:path="c.yml"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["k"], "v")
}

func TestLoad_MissingPath(t *testing.T) {
	_, err := runLoad(t, "", `@config.load`)
	ktest.RequireErrorContains(t, err, "path is required")
}

func TestLoad_MalformedDirective(t *testing.T) {
	_, err := runLoad(t, "", `@config.loa:path="x.yml"`)
	ktest.RequireErrorContains(t, err, "malformed")
}

func TestParseSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want configLoadSpec
	}{
		{"path only", `:path="a.yml"`, configLoadSpec{Path: "a.yml", Required: true}},
		{"path prefix", `:path="a.yml" :prefix="x"`, configLoadSpec{Path: "a.yml", Prefix: "x", Required: true}},
		{"optional", `:path="a.yml" :required=false`, configLoadSpec{Path: "a.yml", Required: false}},
		{"no leading colon", `path="a.yml"`, configLoadSpec{Path: "a.yml", Required: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, parseConfigLoadSpec(c.in), c.want)
		})
	}
}

func TestFlattenConfig_Nested(t *testing.T) {
	out := map[string]string{}
	flattenConfig("app", map[string]any{
		"name": "svc",
		"db":   map[string]any{"host": "localhost"},
	}, out)
	ktest.RequireEqual(t, out["app.name"], "svc")
	ktest.RequireEqual(t, out["app.db.host"], "localhost")
}

func TestFlattenConfig_Array(t *testing.T) {
	out := map[string]string{}
	flattenConfig("", map[string]any{"list": []any{1, 2, 3}}, out)
	ktest.RequireEqual(t, out["list"], "[1,2,3]")
}
