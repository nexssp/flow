package config_yaml

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/config"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestLoadActionLoadsYAMLObject(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.YAML")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("repositories:\n  - nexssp/kernel\nmetadata:\n  source: test\n"), 0o600))

	got, err := LoadAction.DoAny(context.Background(), map[string]any{"path": path})
	ktest.RequireNoError(t, err)
	values, ok := got.(map[string]any)
	ktest.RequireCondition(t, ok, "LoadAction result type = %T, want map[string]any", got)
	ktest.RequireEqual(t, values, map[string]any{
		"repositories": []any{"nexssp/kernel"},
		"metadata":     map[string]any{"source": "test"},
	})
}

func TestLoadActionRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		path string
		data string
		dir  bool
		kind xerr.Kind
	}{
		{name: "empty path", kind: xerr.KindBadRequest},
		{name: "unsupported extension", path: "settings.json", kind: xerr.KindBadRequest},
		{name: "missing file", path: "missing.yaml", kind: xerr.KindNotFound},
		{name: "malformed YAML", path: "bad.yaml", data: "repositories: [\n", kind: xerr.KindBadRequest},
		{name: "non-object YAML", path: "list.yaml", data: "- item\n", kind: xerr.KindBadRequest},
		{name: "empty document", path: "empty.yaml", data: "", kind: xerr.KindBadRequest},
		{name: "read failure", path: "directory.yaml", dir: true, kind: xerr.KindInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := test.path
			if path != "" && !filepath.IsAbs(path) {
				path = filepath.Join(t.TempDir(), path)
			}
			if test.dir {
				ktest.RequireNoError(t, os.Mkdir(path, 0o700))
			} else if test.path != "" && test.name != "missing file" && test.name != "unsupported extension" {
				ktest.RequireNoError(t, os.WriteFile(path, []byte(test.data), 0o600))
			}

			_, err := LoadAction.DoAny(context.Background(), map[string]any{"path": path})
			ktest.RequireNotNil(t, err)
			ktest.RequireEqual(t, xerr.KindFrom(err), test.kind)
		})
	}
}

func TestLoadActionReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LoadAction.DoAny(ctx, map[string]any{"path": filepath.Join(t.TempDir(), "settings.yaml")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadAction error = %v, want context.Canceled", err)
	}
}

func TestLoadYAMLFixture(t *testing.T) {
	source, err := fs.ReadFile(fixturesFS, "nflows/load_yaml.nflow")
	ktest.RequireNoError(t, err)

	workingDir := t.TempDir()
	path := filepath.Join(workingDir, "repopeek.yml")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("repositories:\n  - nexssp/kernel\n  - nexssp/flow\nmetadata:\n  source: fixture\n"), 0o600))
	t.Chdir(workingDir)

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		syntax.Bundle(nil),
		description.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		config.Bundle(nil),
		Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, string(source), "load_yaml.nflow", map[string]any{}, runner.Opts{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("RunSource returned %d; stdout=%q; stderr=%q", code, stdout.String(), stderr.String())
	}
	ktest.RequireEqual(t, stderr.String(), "")
}
