package contracts

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

type PipelineCompiler interface {
	CompilePipeline(expr string) (action.Executable, error)
}

// ActionResolver abstracts capability lookups without coupling to a static Registry.
type ActionResolver interface {
	Action(name string) (action.AnyAction, bool)
}

var (
	actionResolverKey = xctx.NewKey[ActionResolver]("flow.action_resolver")
	compilerKey       = xctx.NewKey[PipelineCompiler]("flow.compiler")
)

func WithActionResolver(ctx context.Context, resolver ActionResolver) context.Context {
	if resolver == nil {
		return ctx
	}
	return actionResolverKey.With(ctx, resolver)
}

func ActionResolverFromContext(ctx context.Context) ActionResolver {
	r, _ := actionResolverKey.From(ctx)
	return r
}

func WithCompiler(ctx context.Context, c PipelineCompiler) context.Context {
	if c == nil {
		return ctx
	}
	return compilerKey.With(ctx, c)
}

func CompilerFromContext(ctx context.Context) PipelineCompiler {
	c, _ := compilerKey.From(ctx)
	return c
}
