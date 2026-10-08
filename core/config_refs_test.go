package core_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func configRefConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	return cfg
}

func TestConfigRef_ValidKeyPasses(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)

	src := `@config {db_url: "postgres://x"}
runtime.const @{ value: "@config.db_url" }`

	_, err := runner.Compile(t.Context(), cfg, src, "ok.nflow")
	ktest.RequireNoError(t, err)
}

func TestConfigRef_UnknownKeyRejected(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)

	src := `@config {db_url: "postgres://x"}
runtime.const @{ value: "@config.db_ur" }`

	_, err := runner.Compile(t.Context(), cfg, src, "typo.nflow")
	ktest.RequireErrorContains(t, err, "unknown @config.db_ur")
	ktest.RequireErrorContains(t, err, "declared: db_url")
	ktest.RequireErrorContains(t, err, "typo.nflow:")
}

func TestConfigRef_UnknownKeyInModifierRejected(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)

	src := `@config {wait: "5s"}
runtime.noop:timeout=@config.waitt`

	_, err := runner.Compile(t.Context(), cfg, src, "mod.nflow")
	ktest.RequireErrorContains(t, err, "modifier :timeout")
	ktest.RequireErrorContains(t, err, "unknown @config.waitt")
}

func TestConfigRef_UnknownKeyInNestedArgRejected(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)

	src := `@config {db_url: "x"}
runtime.const @{ value: { nested: "@config.db_ur" } }`

	_, err := runner.Compile(t.Context(), cfg, src, "nested.nflow")
	ktest.RequireErrorContains(t, err, "arg value.nested")
	ktest.RequireErrorContains(t, err, "unknown @config.db_ur")
}

func TestConfigRef_NoConfigDirectiveSkipsCheck(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)

	// No @config directive: references are not validated (backward compat).
	src := `runtime.const @{ value: "@config.anything" }`

	_, err := runner.Compile(t.Context(), cfg, src, "nocheck.nflow")
	ktest.RequireNoError(t, err)
}

func TestConfigRef_PipelineBodyInheritsParentConfig(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)

	src := `@config {wait: "5s"}

@pipeline work
  runtime.const @{ value: "@config.waitt" }
@end

pipeline.work`

	_, err := runner.Compile(t.Context(), cfg, src, "pipeline.nflow")
	ktest.RequireErrorContains(t, err, "unknown @config.waitt")
}

func TestFlagRef_ValidFlagPasses(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)
	cfg.CompileOpts = append(cfg.CompileOpts, core.WithCLIArgs([]string{"-t=10s"}))

	src := `runtime.noop:timeout=@flag.t`
	_, err := runner.Compile(t.Context(), cfg, src, "ok.nflow")
	ktest.RequireNoError(t, err)
}

func TestFlagRef_UnknownFlagRejected(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)
	cfg.CompileOpts = append(cfg.CompileOpts, core.WithCLIArgs([]string{"-t=10s"}))

	src := `runtime.noop:timeout=@flag.tt`
	_, err := runner.Compile(t.Context(), cfg, src, "typo.nflow")
	ktest.RequireErrorContains(t, err, "unknown @flag.tt")
	ktest.RequireErrorContains(t, err, "available: t")
}

func TestFlagRef_EmptyFlagListRejectsAll(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)
	cfg.CompileOpts = append(cfg.CompileOpts, core.WithCLIArgs([]string{}))

	src := `runtime.noop:timeout=@flag.t`
	_, err := runner.Compile(t.Context(), cfg, src, "none.nflow")
	ktest.RequireErrorContains(t, err, "unknown @flag.t")
	ktest.RequireErrorContains(t, err, "available: none")
}

func TestFlagRef_NilCLIArgsSkipsCheck(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)

	// No WithCLIArgs option: cliArgs stays nil, so @flag references are
	// neither validated nor resolved. Use a string-carrying field so an
	// unresolved reference does not collide with a value-kind check —
	// a duration modifier would reject the empty resolved value before
	// the flag validator even runs.
	src := `runtime.const @{ value: "@flag.anything" }`
	_, err := runner.Compile(t.Context(), cfg, src, "skip.nflow")
	ktest.RequireNoError(t, err)
}

func TestFlagRef_InsideOnErrorGuardIsValidated(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)
	cfg.CompileOpts = append(cfg.CompileOpts, core.WithCLIArgs([]string{"-x=42"}))

	ok := `runtime.fail @{ kind: "Timeout" } ? on_error {
  xerr.KindTimeout -> runtime.const @{ value: "@flag.x" }
}`
	_, err := runner.Compile(t.Context(), cfg, ok, "guard_ok.nflow")
	ktest.RequireNoError(t, err)

	typo := `runtime.fail @{ kind: "Timeout" } ? on_error {
  xerr.KindTimeout -> runtime.const @{ value: "@flag.y" }
}`
	_, err = runner.Compile(t.Context(), cfg, typo, "guard_typo.nflow")
	ktest.RequireErrorContains(t, err, "unknown @flag.y")
	ktest.RequireErrorContains(t, err, "guard_typo.nflow:")
}

func TestFlagRef_InsideOnErrorCaseBodyIsValidated(t *testing.T) {
	t.Parallel()
	cfg := configRefConfig(t)
	cfg.CompileOpts = append(cfg.CompileOpts, core.WithCLIArgs([]string{"-x=42"}))

	// Reference only in a case body, not in Protected — must still be caught.
	src := `runtime.fail ? on_error {
  else -> runtime.const @{ value: "@flag.typo" }
}`
	_, err := runner.Compile(t.Context(), cfg, src, "case_body.nflow")
	ktest.RequireErrorContains(t, err, "unknown @flag.typo")
}
