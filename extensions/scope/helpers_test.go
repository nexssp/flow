package scope_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/modifiers_meta"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/scope"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

// buildConfig assembles the standard scope-test bundle set. Extra
// bundles append after the defaults.
func buildConfig(t *testing.T, extra ...core.Bundle) runner.Config {
	t.Helper()
	bundles := []core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		projection.Bundle(nil),
		modifiers_core.Bundle(nil),
		modifiers_meta.Bundle(nil),
		pipeline.Bundle(nil),
		scope.Bundle(nil),
	}
	bundles = append(bundles, extra...)
	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)
	return cfg
}

func run(t *testing.T, cfg runner.Config, src string) (runner.Execution, error) {
	t.Helper()
	return runner.Execute(context.Background(), cfg, src, "test.nflow", nil)
}

func mustRun(t *testing.T, cfg runner.Config, src string) runner.Execution {
	t.Helper()
	ex, err := run(t, cfg, src)
	ktest.RequireNoError(t, err)
	return ex
}

// sleep renders a `sleep @{ duration_ms: N }` atom.
func sleep(ms int) string {
	return "sleep @{ duration_ms: " + strconv.Itoa(ms) + " }"
}
