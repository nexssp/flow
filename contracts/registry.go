package contracts

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

// Registry is the read-only view of the action set that the compiler
// exposes to nodes that need to look up other actions by name
// (bench.run, distribute.map).
type Registry interface {
	Get(name string) (action.AnyAction, bool)
	Actions() []action.AnyAction
}

// PipelineCompiler compiles a DSL string into an executable action.
// Used by supervisor, which spawns and runs child pipelines on the fly.
type PipelineCompiler interface {
	CompilePipeline(expr string) (action.Executable, error)
}

// Context keys, typed via xctx.Key so retrieval is allocation-free and
// type-safe, and so a nil context is handled without a panic.
var (
	registryKey = xctx.NewKey[Registry]("flow.registry")
	compilerKey = xctx.NewKey[PipelineCompiler]("flow.compiler")
)

// WithRegistry attaches a Registry to ctx. Called by the flow compiler
// before every action execution, so context-aware nodes find it
// automatically.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	if reg == nil {
		return ctx
	}

	return registryKey.With(ctx, reg)
}

// RegistryFromContext returns the Registry attached by WithRegistry,
// or nil when the action is running outside a flow.
func RegistryFromContext(ctx context.Context) Registry {
	reg, _ := registryKey.From(ctx)

	return reg
}

// WithCompiler attaches a PipelineCompiler to ctx.
func WithCompiler(ctx context.Context, c PipelineCompiler) context.Context {
	if c == nil {
		return ctx
	}

	return compilerKey.With(ctx, c)
}

// CompilerFromContext returns the PipelineCompiler attached by
// WithCompiler, or nil.
func CompilerFromContext(ctx context.Context) PipelineCompiler {
	c, _ := compilerKey.From(ctx)

	return c
}
