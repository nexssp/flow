package config_yaml

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/config"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestConfigLoad_YAML(t *testing.T) {
	dir := t.TempDir()
	ymlPath := filepath.Join(dir, "app.yml")
	ktest.RequireNoError(t, os.WriteFile(ymlPath,
		[]byte("host: api.example.test\nport: 8443\n"), 0o600))

	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		config.Bundle(nil),
		Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	src := `@config.load:path="` + filepath.ToSlash(ymlPath) + `"
@assert: result.host == "api.example.test"
runtime.const @{ value: { host: "@config.host" } }`

	ex, err := runner.Execute(context.Background(), cfg, src, "test.nflow", nil)
	ktest.RequireNoError(t, err)

	m, ok := ex.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "expected map output, got %T", ex.Output)
	ktest.RequireEqual(t, m["host"], "api.example.test")
}
