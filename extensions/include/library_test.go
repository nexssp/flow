package include_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/include"
	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

// includeCfg is the minimal bundle set that exercises @include with
// macros. Building it explicitly rather than pulling native.Bundles()
// keeps the test's dependency surface visible and prevents a future
// native change from quietly altering what this test runs against.
func includeCfg(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		include.Bundle(nil),
		macros.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

// TestInclude_MacrosFromIncludedFileAreVisible proves the whole
// merge path end to end: @macro declaration lives in one file, that
// file is @include-d, and the parent uses the macro.
//
// Before mergeIncludedMeta learned to concatenate "macros", the
// included file's declaration was silently dropped whenever the
// parent already had a "macros" key (or when a second @include was
// added), and this test would fail with an unknown-macro parse error.
func TestInclude_MacrosFromIncludedFileAreVisible(t *testing.T) {
	dir := t.TempDir()

	ktest.RequireNoError(t, os.WriteFile(
		filepath.Join(dir, "shared.nflow"),
		[]byte(`@macro shared() { runtime.const @{ value: "shared" } }`+"\n"),
		0o600,
	))

	src := `@include ./shared.nflow
@shared()
`
	ex, err := runner.Execute(context.Background(), includeCfg(t), src,
		filepath.Join(dir, "main.nflow"), nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "shared")
}
