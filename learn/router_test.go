package learn_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/flow/learn"
	"github.com/nexssp/kernel/action"
)

func TestRouter_ExplorationAndConvergence(t *testing.T) {
	t.Parallel()

	var fastCalls, slowCalls atomic.Int32

	fastNode := action.New("provider.fast", func(_ context.Context, req string) (string, error) {
		fastCalls.Add(1)
		time.Sleep(2 * time.Millisecond)

		return req + "_fast", nil
	}).Build()

	slowNode := action.New("provider.slow", func(_ context.Context, req string) (string, error) {
		slowCalls.Add(1)
		time.Sleep(20 * time.Millisecond)

		return req + "_slow", nil
	}).Build()

	rewardFn := func(_ any, err error, d time.Duration) float64 {
		if err != nil {
			return -50.0
		}

		return 50.0 - float64(d.Milliseconds())*2.0
	}

	router := learn.NewRouter(learn.RouterConfig{
		Name:         "rl_router",
		Temperature:  0.8,
		LearningRate: 0.2,
		RewardFn:     rewardFn,
	}, fastNode, slowNode)

	ctx := context.Background()

	// Run 60 trials
	for i := 0; i < 60; i++ {
		_, err := router.DoAny(ctx, "payload")
		if err != nil {
			t.Fatalf("router call failed: %v", err)
		}
	}

	// Policy should converge heavily on fastNode
	if fastCalls.Load() <= slowCalls.Load() {
		t.Fatalf("expected RL router to converge on fast node: fast=%d, slow=%d",
			fastCalls.Load(), slowCalls.Load())
	}
}
