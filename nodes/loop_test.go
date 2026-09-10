package nodes_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

func TestLoop_BreaksWhenConditionMet(t *testing.T) {
	t.Parallel()

	worker := action.New("increment", func(_ context.Context, in map[string]any) (map[string]any, error) {
		count := 0
		if c, ok := in["count"].(int); ok {
			count = c
		} else if cFloat, ok := in["count"].(float64); ok {
			count = int(cFloat)
		}

		newCount := count + 1

		return map[string]any{
			"count": newCount,
			"done":  newCount >= 3,
		}, nil
	}).Build()

	loop, err := nodes.NewLoopAction(worker, "done == true", 5)
	if err != nil {
		t.Fatalf("NewLoopAction failed: %v", err)
	}

	res, err := loop.Do(context.Background(), map[string]any{"count": 0})
	if err != nil {
		t.Fatalf("loop execution failed: %v", err)
	}

	m, ok := res.(map[string]any)
	if !ok || m["count"] != 3 {
		t.Fatalf("expected final count 3, got: %v", res)
	}
}

func TestLoop_ExceedsMaxTurns(t *testing.T) {
	t.Parallel()

	neverDone := action.New("noop", func(_ context.Context, in map[string]any) (map[string]any, error) {
		count := 0
		if c, ok := in["count"].(int); ok {
			count = c
		} else if cFloat, ok := in["count"].(float64); ok {
			count = int(cFloat)
		}

		return map[string]any{"count": count + 1, "done": false}, nil
	}).Build()

	loop, err := nodes.NewLoopAction(neverDone, "done == true", 3)
	if err != nil {
		t.Fatal(err)
	}

	_, err = loop.Do(context.Background(), map[string]any{"count": 0})
	if err == nil {
		t.Fatal("expected loop max turns timeout error")
	}
}
