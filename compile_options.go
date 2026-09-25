package flow

import (
	"context"

	"github.com/nexssp/flow/directives/builtin/at_schema"
	"github.com/nexssp/flow/directives/core"
)

type CompileOption func(*compileOptions)

type compileOptions struct {
	compileCtx   context.Context
	atomAdvisors []core.AtomAdvisor
	flowRegistry *Registry
	config       map[string]string
	cliArgs      []string
	schemas      map[string]at_schema.Schema
}

func applyCompileOptions(opts []CompileOption) *compileOptions {
	c := &compileOptions{compileCtx: context.Background()}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

func WithCompileContext(ctx context.Context) CompileOption {
	return func(c *compileOptions) {
		if ctx != nil {
			c.compileCtx = ctx
		}
	}
}

func WithFlowRegistry(reg *Registry) CompileOption {
	return func(c *compileOptions) {
		c.flowRegistry = reg
	}
}

func WithConfig(cfg map[string]string) CompileOption {
	return func(c *compileOptions) {
		c.config = cfg
	}
}

func WithCLIArgs(args []string) CompileOption {
	return func(c *compileOptions) {
		c.cliArgs = args
	}
}
