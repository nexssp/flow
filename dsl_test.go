package flow_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func TestDSL_PipeAndParallelScatterGather(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	var packCalls, secCalls, archCalls, gateCalls atomic.Int32

	actPack := action.New("pack", func(_ context.Context, _ any) (string, error) {
		packCalls.Add(1)

		return "package auth\nfunc Verify() {}", nil
	}).Build()

	actSec := action.New("security.audit", func(_ context.Context, code string) (string, error) {
		secCalls.Add(1)

		return "Security: Verified", nil
	}).Build()

	actArch := action.New("architecture.review", func(_ context.Context, code string) (string, error) {
		archCalls.Add(1)

		return "Architecture: Decoupled", nil
	}).Build()

	actGate := action.New("review.gate", func(_ context.Context, reports map[string]any) (string, error) {
		gateCalls.Add(1)

		if reports["security.audit"] == nil || reports["architecture.review"] == nil {
			t.Fatal("gate missing expected parallel branch outputs")
		}

		return "Approved for deployment", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(actPack, actSec, actArch, actGate))

	pipeline, err := flow.CompilePipeline("pack | (security.audit & architecture.review) | review.gate", reg)
	if err != nil {
		t.Fatalf("CompilePipeline failed: %v", err)
	}

	res, err := pipeline.Build().Do(ctx, nil)
	if err != nil {
		t.Fatalf("pipeline execution failed: %v", err)
	}

	if res != "Approved for deployment" {
		t.Fatalf("unexpected final output: %v", res)
	}

	if packCalls.Load() != 1 || secCalls.Load() != 1 || archCalls.Load() != 1 || gateCalls.Load() != 1 {
		t.Fatalf("expected all actions to execute exactly once")
	}
}
