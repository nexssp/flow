package flow

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
)

type WorkflowFixture struct {
	t        testing.TB
	dsl      string
	registry *action.Registry
	built    action.Executable
	timeout  time.Duration
}

func NewWorkflowTest(t testing.TB, reg *action.Registry, dsl string) *WorkflowFixture {
	builder, err := CompilePipeline(dsl, reg)
	if err != nil {
		t.Fatalf("workflow_testkit: DSL compilation failed for %q: %v", dsl, err)
	}

	return &WorkflowFixture{
		t:        t,
		dsl:      dsl,
		registry: reg,
		built:    builder.Build(),
		timeout:  5 * time.Second,
	}
}

func (wf *WorkflowFixture) WithTimeout(d time.Duration) *WorkflowFixture {
	wf.timeout = d

	return wf
}

func (wf *WorkflowFixture) Execute(input any) *WorkflowResult {
	ctx, cancel := context.WithTimeout(context.Background(), wf.timeout)
	defer cancel()

	start := time.Now()
	out, err := wf.built.ExecuteDecoded(ctx, func(target any) error {
		if ptr, ok := target.(*any); ok {
			*ptr = input

			return nil
		}

		return nil
	})

	return &WorkflowResult{
		t:        wf.t,
		output:   out,
		err:      err,
		duration: time.Since(start),
	}
}

type WorkflowResult struct {
	t        testing.TB
	output   any
	err      error
	duration time.Duration
}

func (r *WorkflowResult) ExpectSuccess() *WorkflowResult {
	if r.err != nil {
		r.t.Fatalf("workflow expected success, got error: %v", r.err)
	}

	return r
}

func (r *WorkflowResult) ExpectError() *WorkflowResult {
	if r.err == nil {
		r.t.Fatal("workflow expected error, got nil")
	}

	return r
}

func (r *WorkflowResult) Output() any {
	return r.output
}

func (r *WorkflowResult) Duration() time.Duration {
	return r.duration
}
