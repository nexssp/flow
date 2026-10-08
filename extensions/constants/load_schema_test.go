package constants_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/constants"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"

	"github.com/nexssp/flow/core"
)

func constSchemaConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		schema.Bundle(nil),
		constants.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	ktest.RequireNoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestConstLoad_ValidSchemaPasses(t *testing.T) {
	t.Parallel()
	cfg := constSchemaConfig(t)
	path := writeConfig(t, `{"cluster_url":"https://x","max_workers":4}`)

	src := `@schema Infra {
  ClusterURL string ` + "`json:\"cluster_url\" validate:\"required\"`" + `
  MaxWorkers int    ` + "`json:\"max_workers\" validate:\"required\"`" + `
}
@const.load "` + filepath.ToSlash(path) + `" as cfg :schema=Infra
@assert: result.url == "https://x"
runtime.const @{ value: { url: "${cfg.cluster_url}" } }`

	_, err := runner.Execute(t.Context(), cfg, src, "ok.nflow", nil)
	ktest.RequireNoError(t, err)
}

func TestConstLoad_MissingRequiredFieldFailsAtCompile(t *testing.T) {
	t.Parallel()
	cfg := constSchemaConfig(t)
	path := writeConfig(t, `{"cluster_url":"https://x"}`)

	src := `@schema Infra {
  ClusterURL string ` + "`json:\"cluster_url\" validate:\"required\"`" + `
  MaxWorkers int    ` + "`json:\"max_workers\" validate:\"required\"`" + `
}
@const.load "` + filepath.ToSlash(path) + `" as cfg :schema=Infra
runtime.const @{ value: "x" }`

	_, err := runner.Compile(t.Context(), cfg, src, "missing.nflow")
	ktest.RequireErrorContains(t, err, "max_workers")
	ktest.RequireErrorContains(t, err, "required")
	ktest.RequireErrorContains(t, err, "missing.nflow:")
}

func TestConstLoad_WrongTypeFailsAtCompile(t *testing.T) {
	t.Parallel()
	cfg := constSchemaConfig(t)
	path := writeConfig(t, `{"cluster_url":"https://x","max_workers":"many"}`)

	src := `@schema Infra {
  ClusterURL string ` + "`json:\"cluster_url\" validate:\"required\"`" + `
  MaxWorkers int    ` + "`json:\"max_workers\" validate:\"required\"`" + `
}
@const.load "` + filepath.ToSlash(path) + `" as cfg :schema=Infra
runtime.const @{ value: "x" }`

	_, err := runner.Compile(t.Context(), cfg, src, "wrongtype.nflow")
	ktest.RequireErrorContains(t, err, "max_workers")
	ktest.RequireErrorContains(t, err, "number")
}

func TestConstLoad_UnknownSchemaNameFailsAtCompile(t *testing.T) {
	t.Parallel()
	cfg := constSchemaConfig(t)
	path := writeConfig(t, `{"cluster_url":"https://x"}`)

	src := `@schema Infra {
  ClusterURL string ` + "`json:\"cluster_url\"`" + `
}
@const.load "` + filepath.ToSlash(path) + `" as cfg :schema=Infraa
runtime.const @{ value: "x" }`

	_, err := runner.Compile(t.Context(), cfg, src, "unknown.nflow")
	ktest.RequireErrorContains(t, err, "unknown schema")
	ktest.RequireErrorContains(t, err, "Infraa")
	ktest.RequireErrorContains(t, err, "declared: Infra")
}

func TestConstLoad_NoSchemaDeclaredFailsAtCompile(t *testing.T) {
	t.Parallel()
	cfg := constSchemaConfig(t)
	path := writeConfig(t, `{"x":1}`)

	src := `@const.load "` + filepath.ToSlash(path) + `" as cfg :schema=Infra
runtime.const @{ value: "x" }`

	_, err := runner.Compile(t.Context(), cfg, src, "noschema.nflow")
	ktest.RequireErrorContains(t, err, "no @schema declared")
}

func TestConstLoad_NoSchemaModifierUnchanged(t *testing.T) {
	t.Parallel()
	cfg := constSchemaConfig(t)
	path := writeConfig(t, `{"anything":"goes"}`)

	src := `@const.load "` + filepath.ToSlash(path) + `" as cfg
@assert: result.v == "goes"
runtime.const @{ value: { v: "${cfg.anything}" } }`

	_, err := runner.Execute(t.Context(), cfg, src, "plain.nflow", nil)
	ktest.RequireNoError(t, err)
}

func TestConstLoad_SchemaMustAppearAbove(t *testing.T) {
	t.Parallel()
	cfg := constSchemaConfig(t)
	path := writeConfig(t, `{"x":1}`)

	src := `@const.load "` + filepath.ToSlash(path) + `" as cfg :schema=Infra
@schema Infra { X int ` + "`json:\"x\"`" + ` }
runtime.const @{ value: "x" }`

	_, err := runner.Compile(t.Context(), cfg, src, "order.nflow")
	ktest.RequireErrorContains(t, err, "no @schema declared")
}
