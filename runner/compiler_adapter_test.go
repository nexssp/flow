package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/flow/compiler/contracts"
)

func TestCompilerAdapterCompilePipelinePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	executionCtx := withExecutionContext(ctx, Config{}, nil, nil)
	pipelineCompiler := contracts.CompilerFromContext(executionCtx)
	if pipelineCompiler == nil {
		t.Fatal("pipeline compiler is missing from execution context")
	}
	if _, err := pipelineCompiler.CompilePipeline("runtime.noop"); !errors.Is(err, context.Canceled) {
		t.Fatalf("CompilePipeline error = %v, want context.Canceled", err)
	}
}
