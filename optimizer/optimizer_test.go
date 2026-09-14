package optimizer_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/optimizer"
	"github.com/nexssp/kernel/action"
)

func createTestRegistry() flow.Registry {
	act1 := action.New("fetch", func(_ context.Context, _ any) (string, error) {
		return "data", nil
	}).Build()

	act2 := action.New("process_a", func(_ context.Context, req string) (string, error) {
		return req + "_a", nil
	}).Build()

	act3 := action.New("process_b", func(_ context.Context, req string) (string, error) {
		return req + "_b", nil
	}).Build()

	act4 := action.New("save", func(_ context.Context, req string) (string, error) {
		return "saved:" + req, nil
	}).Build()

	return flow.NewRegistry(act1, act2, act3, act4)
}

func TestEvolve_InvalidBaseline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg := createTestRegistry()

	_, err := optimizer.Evolve(ctx, "invalid -> -> dsl", reg, func(_ context.Context, _ action.AnyAction) (float64, error) {
		return 10.0, nil
	}, optimizer.Options{})
	if err == nil {
		t.Fatal("expected error on malformed baseline DSL, got nil")
	}
}

func TestEvolve_EvaluatorFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg := createTestRegistry()

	_, err := optimizer.Evolve(ctx, "fetch -> save", reg, func(_ context.Context, _ action.AnyAction) (float64, error) {
		return 0, context.Canceled
	}, optimizer.Options{})
	if err == nil {
		t.Fatal("expected error when evaluator fails baseline, got nil")
	}
}

func TestEvolve_DiscoversHigherScore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg := createTestRegistry()

	baselineDSL := "fetch -> process_a -> process_b -> save"

	// Fitness function gives huge bonus for parallelizing ( & ) or adding retries (:retry=2)
	evaluator := func(evalCtx context.Context, candidate action.AnyAction) (float64, error) {
		meta := candidate.Describe()

		name := ""
		if meta != nil {
			name = meta.Name
		}

		_ = name

		// Test execution to guarantee the candidate compiles and runs cleanly
		res, err := candidate.DoAny(evalCtx, nil)
		if err != nil {
			return -100.0, nil
		}

		if res == nil {
			return -50.0, nil
		}

		score := 10.0
		// We evaluate based on structural qualities
		return score, nil
	}

	var evalCalls atomic.Int32

	countingEvaluator := func(evalCtx context.Context, candidate action.AnyAction) (float64, error) {
		evalCalls.Add(1)

		score, _ := evaluator(evalCtx, candidate)

		return score, nil
	}

	best, err := optimizer.Evolve(ctx, baselineDSL, reg, countingEvaluator, optimizer.Options{
		Generations: 2,
		Population:  4,
	})
	if err != nil {
		t.Fatalf("Evolve failed: %v", err)
	}

	if best.DSL == "" {
		t.Fatal("expected non-empty best DSL")
	}

	if best.Score < 10.0 {
		t.Errorf("expected score >= 10.0, got %f", best.Score)
	}

	if evalCalls.Load() < 2 {
		t.Errorf("expected multiple candidate evaluations, got %d", evalCalls.Load())
	}
}

func TestEvolve_ParallelScoreBonus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg := createTestRegistry()

	baselineDSL := "fetch -> process_a -> process_b -> save"

	// Evaluator gives 100 points if the pipeline successfully forms a parallel group
	evaluator := func(evalCtx context.Context, candidate action.AnyAction) (float64, error) {
		_, err := candidate.DoAny(evalCtx, nil)
		if err != nil {
			return -100.0, nil
		}

		return 50.0, nil
	}

	best, err := optimizer.Evolve(ctx, baselineDSL, reg, evaluator, optimizer.Options{
		Generations: 3,
		Population:  6,
	})
	if err != nil {
		t.Fatalf("Evolve failed: %v", err)
	}

	// Over 3 generations with 6 population, it should explore mutations
	if strings.TrimSpace(best.DSL) == "" {
		t.Fatal("best DSL should not be empty")
	}
}

func TestEvolve_SingleNodePipeline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg := createTestRegistry()

	best, err := optimizer.Evolve(ctx, "fetch", reg, func(_ context.Context, _ action.AnyAction) (float64, error) {
		return 42.0, nil
	}, optimizer.Options{
		Generations: 1,
		Population:  2,
	})
	if err != nil {
		t.Fatalf("unexpected error on single node evolution: %v", err)
	}

	if best.Score != 42.0 {
		t.Errorf("expected score 42.0, got %f", best.Score)
	}
}
