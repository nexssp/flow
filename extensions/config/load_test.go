package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func runLoad(tb testing.TB, baseDir, line string) (map[string]string, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleConfigLoad(context.Background(), compiler.DirectiveReq{
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

func TestLoad_JSON(t *testing.T) {
	path := writeFixture(t, "nexss.json", `{"host":"x","nested":{"deep":"y"}}`)
	cfg, err := runLoad(t, "", `@config.load:path="`+path+`"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["host"], "x")
	ktest.RequireEqual(t, cfg["nested.deep"], "y")
}

func TestDecodeFile_PreservesStructuredJSON(t *testing.T) {
	cfg, err := DecodeFile([]byte(`{"nested":{"enabled":true,"ports":[80,443]}}`), ".JSON")
	ktest.RequireNoError(t, err)
	nested, ok := cfg["nested"].(map[string]any)
	ktest.RequireCondition(t, ok, "nested value has type %T, want map[string]any", cfg["nested"])
	enabled, ok := nested["enabled"].(bool)
	ktest.RequireCondition(t, ok, "enabled value has type %T, want bool", nested["enabled"])
	ktest.RequireEqual(t, enabled, true)
	ports, ok := nested["ports"].([]any)
	ktest.RequireCondition(t, ok, "ports value has type %T, want []any", nested["ports"])
	ktest.RequireEqual(t, ports, []any{float64(80), float64(443)})
}

func TestDecodeFile_JSONRejectsNullAndAllowsEmptyObject(t *testing.T) {
	if _, err := DecodeFile([]byte(`null`), ".json"); err == nil {
		t.Fatal("DecodeFile accepted a top-level JSON null")
	}
	cfg, err := DecodeFile([]byte(`{}`), ".json")
	ktest.RequireNoError(t, err)
	ktest.RequireNotNil(t, cfg)
	ktest.RequireEqual(t, len(cfg), 0)
}

func TestDecodeFile_YAMLRejectsNullAndAllowsEmptyObject(t *testing.T) {
	previous := yamlLoader.Load()
	RegisterYAMLLoader(func(data []byte) (map[string]any, error) {
		if string(data) == "null" {
			return nil, nil
		}
		return map[string]any{}, nil
	})
	defer yamlLoader.Store(previous)

	if _, err := decodeConfigFile([]byte("null"), ".yaml"); err == nil {
		t.Fatal("decodeConfigFile accepted a top-level YAML null")
	}
	cfg, err := decodeConfigFile([]byte("{}"), ".yaml")
	ktest.RequireNoError(t, err)
	ktest.RequireNotNil(t, cfg)
	ktest.RequireEqual(t, len(cfg), 0)
}

func TestLoad_TOML(t *testing.T) {
	path := writeFixture(t, "nexss.toml", "[db]\nhost = \"localhost\"\nport = 5432\n")
	cfg, err := runLoad(t, "", `@config.load:path="`+path+`"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["db.host"], "localhost")
	ktest.RequireEqual(t, cfg["db.port"], "5432")
}

func TestLoad_Prefix(t *testing.T) {
	path := writeFixture(t, "a file.json", `{"x":1}`)
	cfg, err := runLoad(t, "", `@config.load:path="`+path+`" :prefix="my app"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["my app.x"], "1")
}

func TestLoad_MissingOptional(t *testing.T) {
	cfg, err := runLoad(t, "", `@config.load:path="/nonexistent/x.json" :required=false`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(cfg), 0)
}

func TestLoad_MissingRequired(t *testing.T) {
	_, err := runLoad(t, "", `@config.load:path="/nonexistent/x.json"`)
	ktest.RequireErrorContains(t, err, "read")
}

func TestLoad_InvalidRequired(t *testing.T) {
	_, err := runLoad(t, "", `@config.load:path="/nonexistent/x.json" :required=maybe`)
	ktest.RequireErrorContains(t, err, "invalid required value")
}

func TestLoad_RelativeToBaseDir(t *testing.T) {
	dir := t.TempDir()
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(dir, "c.json"), []byte(`{"k":"v"}`), 0o600))
	cfg, err := runLoad(t, dir, `@config.load:path="c.json"`)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg["k"], "v")
}

func TestLoad_MissingPath(t *testing.T) {
	_, err := runLoad(t, "", `@config.load`)
	ktest.RequireErrorContains(t, err, "path is required")
}

func TestLoad_MalformedDirective(t *testing.T) {
	_, err := runLoad(t, "", `@config.loa:path="x.json"`)
	ktest.RequireErrorContains(t, err, "malformed")
}

func TestLoad_FlattenedDottedKeyCollisionIsPositioned(t *testing.T) {
	path := writeFixture(t, "collision.json", `{"service":{"name":"nested"},"service.name":"dotted"}`)
	line := `@config.load:path="` + path + `"`
	_, err := handleConfigLoad(context.Background(), compiler.DirectiveReq{
		Lines:   []string{line},
		I:       0,
		Out:     map[string]any{},
		File:    "main.nflow",
		BaseDir: "",
	})
	ktest.RequireErrorContains(t, err, "flattened config key collision")
	position, ok := compiler.PositionOf(err)
	ktest.RequireCondition(t, ok, "expected a source-positioned error, got %T: %v", err, err)
	ktest.RequireEqual(t, position.File, "main.nflow")
	ktest.RequireEqual(t, position.Line, 1)
}

func TestFlattenConfig_RejectsEmptyKeyComponentsAtomically(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value map[string]any
	}{
		{"root empty key", map[string]any{"": "value"}},
		{"nested empty key", map[string]any{"parent": map[string]any{"": "value"}}},
		{"empty dotted component", map[string]any{"parent..child": "value"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			out := map[string]string{"keep": "original"}
			err := flattenConfig("", test.value, out)
			ktest.RequireErrorContains(t, err, "empty component")
			ktest.RequireEqual(t, out, map[string]string{"keep": "original"})
		})
	}
}

func TestFlattenConfig_RejectsCollisionWithPrepopulatedOutputAtomically(t *testing.T) {
	out := map[string]string{"existing": "original", "keep": "unchanged"}
	err := flattenConfig("", map[string]any{
		"existing": "replacement",
		"new":      "value",
	}, out)
	ktest.RequireErrorContains(t, err, "flattened config key collision")
	ktest.RequireEqual(t, out, map[string]string{"existing": "original", "keep": "unchanged"})
}

func TestFlattenConfig_InternalCollisionDoesNotPartiallyMerge(t *testing.T) {
	out := map[string]string{"keep": "original"}
	err := flattenConfig("", map[string]any{
		"service":      map[string]any{"name": "nested"},
		"service.name": "dotted",
		"new":          "value",
	}, out)
	ktest.RequireErrorContains(t, err, "flattened config key collision")
	ktest.RequireEqual(t, out, map[string]string{"keep": "original"})
}

func TestLoad_SequentialSourcesRejectDuplicateFlattenedKey(t *testing.T) {
	first := writeFixture(t, "first.json", `{"service":{"name":"first"},"firstOnly":"kept"}`)
	second := writeFixture(t, "second.json", `{"service.name":"second","secondOnly":"not merged"}`)
	out := map[string]any{}
	load := func(path string) error {
		_, err := handleConfigLoad(context.Background(), compiler.DirectiveReq{
			Lines:   []string{`@config.load:path="` + path + `"`},
			I:       0,
			Out:     out,
			File:    "main.nflow",
			BaseDir: "",
		})
		return err
	}
	ktest.RequireNoError(t, load(first))
	err := load(second)
	ktest.RequireErrorContains(t, err, "flattened config key collision")
	position, ok := compiler.PositionOf(err)
	ktest.RequireCondition(t, ok, "expected a source-positioned error, got %T: %v", err, err)
	ktest.RequireEqual(t, position.File, "main.nflow")
	ktest.RequireEqual(t, position.Line, 1)
	cfg, ok := out["config"].(map[string]string)
	ktest.RequireCondition(t, ok, "config has type %T, want map[string]string", out["config"])
	ktest.RequireEqual(t, cfg, map[string]string{
		"service.name": "first",
		"firstOnly":    "kept",
	})
}

func TestLoad_YAMLWithoutBundle(t *testing.T) {
	path := writeFixture(t, "cfg.yml", "k: v\n")
	_, err := runLoad(t, "", `@config.load:path="`+path+`"`)
	ktest.RequireErrorContains(t, err, "requires @require config_yaml")
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
		{"quoted spaces", `:path="a file.yml" :prefix="my app"`, configLoadSpec{Path: "a file.yml", Prefix: "my app", Required: true}},
		{"optional", `:path="a.yml" :required=false`, configLoadSpec{Path: "a.yml", Required: false}},
		{"no leading colon", `path="a.yml"`, configLoadSpec{Path: "a.yml", Required: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseConfigLoadSpec(c.in)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestParseSpecRejectsInvalidKnownOptions(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`:required=maybe`,
		`:required`,
		`:required=true :required=false`,
		`:path="unterminated`,
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := parseConfigLoadSpec(input)
			if err == nil {
				t.Fatal("parseConfigLoadSpec succeeded for invalid option")
			}
		})
	}
}

func TestDecodeFileRejectsMalformedTOML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"malformed line", "[db]\nhost = \"db\"\nport 5432\n", "expected key = value"},
		{"duplicate key", "[db]\nhost = \"first\"\nhost = \"second\"\n", "duplicate key"},
		{"duplicate table", "[db]\nhost = \"db\"\n[db]\n", "duplicate table"},
		{"malformed table", "[db\nhost = \"db\"\n", "malformed table header"},
		{"table value conflict", "db = 1\n[db.child]\nx = 2\n", "conflicts with a value"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeFile([]byte(test.input), ".toml")
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestFlattenConfig_Nested(t *testing.T) {
	out := map[string]string{}
	err := flattenConfig("app", map[string]any{
		"name": "svc",
		"db":   map[string]any{"host": "localhost"},
	}, out)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, out["app.name"], "svc")
	ktest.RequireEqual(t, out["app.db.host"], "localhost")
}

func TestFlattenConfig_Array(t *testing.T) {
	out := map[string]string{}
	err := flattenConfig("", map[string]any{"list": []any{1, 2, 3}}, out)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, out["list"], "[1,2,3]")
}
