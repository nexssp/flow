package config_runtime

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
	configyamlext "github.com/nexssp/flow/extensions/config_yaml"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestLoadJSONFixture(t *testing.T) {
	executeFixture(t, "load_json.nflow", "load_json--settings.json", "settings.json")
}

func TestLoadYAMLFixture(t *testing.T) {
	executeFixture(t, "load_yaml.nflow", "load_yaml--settings.yml", "settings.yml")
}

func executeFixture(t *testing.T, fixtureName, companionName, runtimeName string) {
	t.Helper()
	fixtures := Bundle(nil).Fixtures
	source, err := fs.ReadFile(fixtures, "nflows/"+fixtureName)
	if err != nil {
		t.Fatalf("read fixture %q: %v", fixtureName, err)
	}
	companion, err := fs.ReadFile(fixtures, "nflows/"+companionName)
	if err != nil {
		t.Fatalf("read companion %q: %v", companionName, err)
	}

	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, runtimeName), companion, 0o600); err != nil {
		t.Fatalf("write runtime fixture: %v", err)
	}
	bundle, err := BundleWithRoot(rootPath)
	if err != nil {
		t.Fatalf("BundleWithRoot: %v", err)
	}
	closeBundle(t, bundle)

	bundles := native.Bundles()
	bundles = append(bundles, configyamlext.Bundle(nil), bundle)
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if _, err := runner.Execute(t.Context(), cfg, string(source), fixtureName, nil); err != nil {
		t.Fatalf("execute %s: %v", fixtureName, err)
	}
}

func TestBundleWithoutHostRootFailsClosed(t *testing.T) {
	bundle := Bundle(nil)
	if len(bundle.AcceptedOptions) != 0 {
		t.Fatalf("source-level options = %v, want none", bundle.AcceptedOptions)
	}
	bundles := append(native.Bundles(), bundle)
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	_, err = runner.Execute(t.Context(), cfg,
		`config_runtime.load @{ path: "settings.json" }`, "unconfigured.nflow", nil)
	if xerr.KindFrom(err) != xerr.KindUnavailable {
		t.Fatalf("execution error = %v, want unavailable without a host root", err)
	}
}

func TestLoadRejectsPathTraversal(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "root")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(parent, "outside.json")
	if err := os.WriteFile(outsidePath, []byte(`{"secret":"outside"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, cfg := buildConfigWithRoot(t, rootPath)
	closeBundle(t, bundle)
	for _, path := range []string{"../outside.json", filepath.ToSlash(outsidePath)} {
		source := `config_runtime.load @{ path: "` + path + `" }`
		_, err := runner.Execute(t.Context(), cfg, source, "traversal.nflow", nil)
		if xerr.KindFrom(err) != xerr.KindForbidden {
			t.Errorf("load path %q error = %v, want forbidden path escape", path, err)
		}
	}
}

func TestLoadRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "root")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(parent, "outside.json")
	outsideContents := `{"secret":"must-not-be-read"}`
	if err := os.WriteFile(outsidePath, []byte(outsideContents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(rootPath, "escape.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	bundle, cfg := buildConfigWithRoot(t, rootPath)
	closeBundle(t, bundle)
	_, err := runner.Execute(t.Context(), cfg,
		`config_runtime.load @{ path: "escape.json" }`, "symlink.nflow", nil)
	if err == nil {
		t.Fatal("loading a symlink outside the host root unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), outsideContents) {
		t.Fatal("error exposed content from outside the host root")
	}
}

func TestBundleWithRootRequiresExplicitDirectory(t *testing.T) {
	if _, err := BundleWithRoot(" "); err == nil {
		t.Fatal("empty host root unexpectedly succeeded")
	}
	if _, err := BundleWithRoot(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing host root unexpectedly succeeded")
	}
}

func buildConfigWithRoot(t *testing.T, rootPath string) (compiler.Bundle, runner.Config) {
	t.Helper()
	bundle, err := BundleWithRoot(rootPath)
	if err != nil {
		t.Fatalf("BundleWithRoot: %v", err)
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	return bundle, cfg
}

func closeBundle(t *testing.T, bundle compiler.Bundle) {
	t.Helper()
	t.Cleanup(func() {
		for _, shutdown := range slices.Backward(bundle.Shutdowns) {
			if err := shutdown(context.Background()); err != nil {
				t.Errorf("close config_runtime bundle: %v", err)
			}
		}
	})
}
