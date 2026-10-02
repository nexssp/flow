package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/config"
)

type sample struct {
	Name  string `yaml:"name"  json:"name"`
	Port  int    `yaml:"port"  json:"port"`
	Debug bool   `yaml:"debug" json:"debug"`
}

// ── Find ─────────────────────────────────────────────────────────────

func TestFind_WalksUpToRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "cfg.yml")
	if err := os.WriteFile(want, []byte("name: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ktest.RequireEqual(t, config.Find("cfg.yml", deep), want)
}

func TestFind_ReturnsEmptyWhenMissing(t *testing.T) {
	t.Parallel()

	ktest.RequireEqual(t, config.Find("nope.yml", t.TempDir()), "")
}

func TestFind_SkipsDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "cfg.yml"), 0o755); err != nil {
		t.Fatal(err)
	}

	ktest.RequireEqual(t, config.Find("cfg.yml", dir), "")
}

func TestFind_EmptyFilename(t *testing.T) {
	t.Parallel()

	ktest.RequireEqual(t, config.Find("", t.TempDir()), "")
}

// ── Load ─────────────────────────────────────────────────────────────

func TestLoad_YAML(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "cfg.yml", "name: server\nport: 8080\ndebug: true\n")

	var cfg sample
	ktest.RequireNoError(t, config.Load(&cfg, path))
	ktest.RequireEqual(t, cfg, sample{Name: "server", Port: 8080, Debug: true})
}

func TestLoad_JSON(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "cfg.json", `{"name":"server","port":8080}`)

	var cfg sample
	ktest.RequireNoError(t, config.Load(&cfg, path))
	ktest.RequireEqual(t, cfg, sample{Name: "server", Port: 8080})
}

func TestLoad_LayeredOverlay(t *testing.T) {
	t.Parallel()

	defaults := writeFile(t, "defaults.yml", "name: default\nport: 80\ndebug: true\n")
	user := writeFile(t, "user.yml", "port: 8080\n")

	var cfg sample
	ktest.RequireNoError(t, config.Load(&cfg, defaults, user))
	ktest.RequireEqual(t, cfg, sample{Name: "default", Port: 8080, Debug: true})
}

func TestLoad_RejectsUnknownField(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "cfg.yml", "name: x\ntypo_field: y\n")

	var cfg sample
	ktest.RequireErrorKind(t, config.Load(&cfg, path), xerr.KindValidation)
}

func TestLoad_RejectsUnsupportedExtension(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "cfg.toml", "name = \"x\"\n")

	var cfg sample
	ktest.RequireErrorKind(t, config.Load(&cfg, path), xerr.KindBadRequest)
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()

	var cfg sample
	err := config.Load(&cfg, filepath.Join(t.TempDir(), "absent.yml"))
	ktest.RequireErrorKind(t, err, xerr.KindNotFound)
}

func TestLoad_EmptyFileIsNoOp(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "empty.yml", "")

	cfg := sample{Name: "preserved", Port: 42}
	ktest.RequireNoError(t, config.Load(&cfg, path))
	ktest.RequireEqual(t, cfg, sample{Name: "preserved", Port: 42})
}

func TestLoad_SkipsEmptyPath(t *testing.T) {
	t.Parallel()

	cfg := sample{Name: "preserved"}
	ktest.RequireNoError(t, config.Load(&cfg, "", ""))
	ktest.RequireEqual(t, cfg, sample{Name: "preserved"})
}

// ── LoadFromDir ──────────────────────────────────────────────────────

func TestLoadFromDir_UsesFoundFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deep := filepath.Join(root, "x", "y")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cfg.yml"), []byte("port: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var cfg sample
	ktest.RequireNoError(t, config.LoadFromDir(&cfg, "cfg.yml", deep))
	ktest.RequireEqual(t, cfg, sample{Port: 8080})
}

func TestLoadFromDir_SilentWhenMissing(t *testing.T) {
	t.Parallel()

	cfg := sample{Name: "default"}
	ktest.RequireNoError(t, config.LoadFromDir(&cfg, "cfg.yml", t.TempDir()))
	ktest.RequireEqual(t, cfg, sample{Name: "default"})
}

// ── helpers ──────────────────────────────────────────────────────────

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
