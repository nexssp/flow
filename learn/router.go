package learn

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
)

type RewardFunc func(res any, err error, duration time.Duration) float64

type routeStat struct {
	Action action.AnyAction
	Weight float64
	Calls  int
}

type RouterConfig struct {
	Name         string
	Temperature  float64
	LearningRate float64
	RewardFn     RewardFunc
}

func NewRouter(cfg RouterConfig, candidates ...action.AnyAction) action.AnyAction {
	if cfg.Temperature <= 0 {
		cfg.Temperature = 1.0
	}

	if cfg.LearningRate <= 0 {
		cfg.LearningRate = 0.1
	}

	if cfg.RewardFn == nil {
		cfg.RewardFn = func(_ any, err error, d time.Duration) float64 {
			if err != nil {
				return -10.0
			}

			return 10.0 - (float64(d.Milliseconds()) / 100.0)
		}
	}

	stats := make([]*routeStat, len(candidates))
	for i, a := range candidates {
		stats[i] = &routeStat{
			Action: a,
			Weight: 0.0,
			Calls:  0,
		}
	}

	var (
		mu         sync.RWMutex
		totalCalls uint64
	)

	return action.New(cfg.Name, func(ctx context.Context, input any) (any, error) {
		mu.Lock()
		n := len(stats)

		var selected *routeStat

		unvisitedIdx := -1

		for i := 0; i < n; i++ {
			if stats[i].Calls == 0 {
				unvisitedIdx = i

				break
			}
		}

		if unvisitedIdx != -1 {
			selected = stats[unvisitedIdx]
		} else {
			var (
				stackProbs [8]float64
				probs      []float64
			)
			if n <= len(stackProbs) {
				probs = stackProbs[:n]
			} else {
				probs = make([]float64, n)
			}

			maxQ := -math.MaxFloat64
			for i := 0; i < n; i++ {
				if stats[i].Weight > maxQ {
					maxQ = stats[i].Weight
				}
			}

			sum := 0.0

			for i := 0; i < n; i++ {
				probs[i] = math.Exp((stats[i].Weight - maxQ) / cfg.Temperature)
				sum += probs[i]
			}

			//nolint:gosec // Fast PRNG is intentional for softmax exploration
			pick := rand.Float64() * sum
			selectedIndex := n - 1

			accum := 0.0
			for i := 0; i < n; i++ {
				accum += probs[i]
				if pick <= accum {
					selectedIndex = i

					break
				}
			}

			selected = stats[selectedIndex]
		}

		totalCalls++
		mu.Unlock()

		start := time.Now()

		exec, ok := selected.Action.(action.Executable)
		if !ok {
			return nil, fmt.Errorf("learning router: action %q is not executable", selected.Action.Describe().Name)
		}

		out, err := exec.ExecuteDecoded(ctx, func(target any) error {
			if ptr, isAnyPtr := target.(*any); isAnyPtr {
				*ptr = input
			}

			return nil
		})
		duration := time.Since(start)

		reward := cfg.RewardFn(out, err, duration)

		mu.Lock()
		selected.Calls++
		selected.Weight += cfg.LearningRate * (reward - selected.Weight)
		mu.Unlock()

		return out, err
	}).
		Description(fmt.Sprintf("High-throughput RL router managing %d paths", len(candidates))).
		Build()
}
