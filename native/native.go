package native

import (
	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/config"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/external"
	"github.com/nexssp/flow/extensions/fs"
	"github.com/nexssp/flow/extensions/include"
	"github.com/nexssp/flow/extensions/loop"
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
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/render"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/extensions/retry"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/extensions/syntax"
)

func Bundles() []core.Bundle {
	return []core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		modifiers_core.Bundle(nil),
		modifiers_auth.Bundle(nil),
		modifiers_meta.Bundle(nil),
		pipeline.Bundle(nil),
		pool.Bundle(nil),
		config.Bundle(nil),
		schema.Bundle(nil),
		projection.Bundle(nil),
		description.Bundle(nil),
		include.Bundle(nil),
		on.Bundle(nil),
		require.Bundle(nil),
		loop.Bundle(nil),
		assert.Bundle(nil),
		external.Bundle(nil),
		fs.Bundle(nil),
		render.Bundle(nil),
		on_error.Bundle(nil),
		retry.Bundle(nil),
		nodes_log.Bundle(nil),
		nodes_bench.Bundle(nil),
		nodes_distribute.Bundle(nil),
		nodes_dispatch.Bundle(nil),
		nodes_supervisor.Bundle(nil),
	}
}

func Primaries() []core.PrimaryExtension {
	var out []core.PrimaryExtension
	bundles := Bundles()
	for i := range bundles {
		out = append(out, bundles[i].Primaries...)
	}
	return out
}

func Directives() *core.DirectiveTable {
	var out []core.Directive
	bundles := Bundles()
	for i := range bundles {
		out = append(out, bundles[i].Directives...)
	}
	return core.NewDirectiveTable(out...)
}
