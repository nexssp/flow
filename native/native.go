// Package native is the single source of truth for the bundle set the
// shipped nflow CLI loads.
//
// Every CLI subcommand that touches a .nflow file — run, lint, expand,
// build, catalog, list, show, and selftest — must build its runner
// config from Bundles() or SelftestBundles(). No command constructs its
// own list. No command calls compiler.RegisteredBundles().
//
// SelftestBundles adds the fixture-only helpers and optional extensions
// needed to exercise repository packages. These are not loaded into ordinary
// flow runs unless the source explicitly requires the extension.
//
// If you find yourself wanting a third list, don't. Either add the
// bundle to Bundles() so it is always present, or register it and load
// it via @require. A new list means a new environment, and that is the
// class of bug this package exists to prevent.
package native

import (
	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/approval"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/config"
	"github.com/nexssp/flow/extensions/config_yaml"
	"github.com/nexssp/flow/extensions/constants"
	"github.com/nexssp/flow/extensions/data"
	"github.com/nexssp/flow/extensions/decide"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/external"
	"github.com/nexssp/flow/extensions/flow_version"
	"github.com/nexssp/flow/extensions/fs"
	"github.com/nexssp/flow/extensions/hook"
	"github.com/nexssp/flow/extensions/include"
	"github.com/nexssp/flow/extensions/io"
	"github.com/nexssp/flow/extensions/loop"
	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/extensions/match"
	"github.com/nexssp/flow/extensions/modifiers_auth"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/modifiers_meta"
	"github.com/nexssp/flow/extensions/nodes_bench"
	"github.com/nexssp/flow/extensions/nodes_dispatch"
	"github.com/nexssp/flow/extensions/nodes_distribute"
	"github.com/nexssp/flow/extensions/nodes_log"
	"github.com/nexssp/flow/extensions/nodes_supervisor"
	"github.com/nexssp/flow/extensions/on"
	"github.com/nexssp/flow/extensions/on_error"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/pool"
	"github.com/nexssp/flow/extensions/progress"
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/realtime"
	"github.com/nexssp/flow/extensions/render"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/extensions/resilience"
	"github.com/nexssp/flow/extensions/retry"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/extensions/scope"
	"github.com/nexssp/flow/extensions/selftestkit"
	"github.com/nexssp/flow/extensions/stream_ops"
	"github.com/nexssp/flow/extensions/symbols"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/extensions/template"
)

// Bundles returns the complete set of bundles the shipped CLI loads.
//
// Do not append to Bundles() from a caller. Every bundle the CLI has
// is already in this list. A caller that needs an additional bundle
// either uses @require in the source or builds its own config from
// scratch — never both, because BuildConfig rejects a duplicate ID.
func Bundles() []compiler.Bundle {
	return bundles(retry.Bundle(nil))
}

// BundlesWithRetryConfig returns the standard native set with a caller-selected
// retry policy. All other bundles and defaults are identical to Bundles.
func BundlesWithRetryConfig(cfg retry.Config) []compiler.Bundle {
	return bundles(retry.BundleWithConfig(cfg))
}

func bundles(retryBundle compiler.Bundle) []compiler.Bundle {
	return []compiler.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		symbols.Bundle(nil),
		match.Bundle(nil),
		modifiers_core.Bundle(nil),
		modifiers_auth.Bundle(nil),
		modifiers_meta.Bundle(nil),
		pipeline.Bundle(nil),
		pool.Bundle(nil),
		config.Bundle(nil),
		constants.Bundle(nil),
		flow_version.Bundle(nil),
		schema.Bundle(nil),
		projection.Bundle(nil),
		progress.Bundle(nil),
		description.Bundle(nil),
		include.Bundle(nil),
		hook.Bundle(nil),
		macros.Bundle(nil),
		on.Bundle(nil),
		require.Bundle(nil),
		loop.Bundle(nil),
		assert.Bundle(nil),
		approval.Bundle(nil),
		external.Bundle(nil),
		fs.Bundle(nil),
		data.Bundle(nil),
		io.Bundle(nil),
		render.Bundle(nil),
		on_error.Bundle(nil),
		retryBundle,
		nodes_log.Bundle(nil),
		nodes_bench.Bundle(nil),
		nodes_distribute.Bundle(nil),
		nodes_dispatch.Bundle(nil),
		nodes_supervisor.Bundle(nil),
		scope.Bundle(nil),
		realtime.Bundle(nil),
		stream_ops.Bundle(nil),
	}
}

// SelftestBundles returns Bundles plus bundles used only for conformance
// testing. Optional extensions are deliberately excluded from Bundles(),
// but their fixtures still run here so every repository extension has a
// verified self-test suite.
//
// selftestkit ships cov.* actions and a cov.items stream source. Its
// fixtures live in other bundles' nflows/ directories and reference
// those actions. A production pipeline must never see cov.*, so
// selftestkit is not in Bundles(). A self-test fixture must always find
// them, so it is here. Optional in-tree bundles such as config_yaml are
// also included here so their conformance fixtures run without making the
// capabilities part of the default bundle set. Language execution is supplied
// by github.com/nexssp/lang/nexssflow through the external @require harness.
//
// These bundles are not in the ordinary CLI set unless explicitly required.
// Keep this set aligned with extension packages so the self-test environment
// can compile and execute every extension fixture.
func SelftestBundles() []compiler.Bundle {
	return SelftestBundlesFrom(Bundles())
}

// SelftestBundlesFrom adds the self-test-only bundle to an existing native
// bundle set. Callers that own bundle lifetimes can reuse their cached set
// instead of invoking every native factory a second time.
func SelftestBundlesFrom(bundles []compiler.Bundle) []compiler.Bundle {
	out := append([]compiler.Bundle(nil), bundles...)
	return append(out,
		selftestkit.Bundle(nil),
		config_yaml.Bundle(nil),
		decide.Bundle(nil),
		resilience.Bundle(nil),
		template.Bundle(nil),
	)
}

// Primaries returns the primary extensions contributed by the native
// set. It is the table a caller reaches for when it needs to parse a
// source fragment without going through CompileAction — for example, a
// diagnostic tool that wants the parser surface but not the runner.
func Primaries() []compiler.PrimaryExtension {
	var out []compiler.PrimaryExtension
	bundles := Bundles()
	for i := range bundles {
		out = append(out, bundles[i].Primaries...)
	}
	return out
}

// Directives returns the standard directive table. It includes @macro,
// @scope, @pipeline, @include, @require, and every other directive the
// shipped CLI understands.
func Directives() *compiler.DirectiveTable {
	var out []compiler.Directive
	bundles := Bundles()
	for i := range bundles {
		out = append(out, bundles[i].Directives...)
	}
	return compiler.NewDirectiveTable(out...)
}
