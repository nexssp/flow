package contracts

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

type PipelineCompiler interface {
	CompilePipeline(expr string) (action.Executable, error)
}

var (
	registryKey = xctx.NewKey[*action.Registry]("flow.registry")
	compilerKey = xctx.NewKey[PipelineCompiler]("flow.compiler")
)

func WithRegistry(ctx context.Context, reg *action.Registry) context.Context {
	if reg == nil {
		return ctx
	}
	return registryKey.With(ctx, reg)
}

func RegistryFromContext(ctx context.Context) *action.Registry {
	reg, _ := registryKey.From(ctx)
	return reg
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
