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

// RewardFunc evaluates the execution of a route and returns a scalar reward.
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

// NewRouter creates a high-throughput, low-latency Softmax reinforcement learning router.
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
		}
	}

	var mu sync.RWMutex

	return action.New(cfg.Name, func(ctx context.Context, input any) (any, error) {
		// --- 1. Selection Phase ---
		// Stack buffer for typical candidate sizes (<= 8) to avoid heap allocation
		var stackProbs [8]float64
		var probs []float64

		mu.RLock()
		n := len(stats)
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

		// Fast, non-blocking pseudo-random float (0 syscalls)
		//nolint:gosec // MAB routing optimization does not require cryptographic security
		pick := rand.Float64() * sum
		selectedIndex := 0
		accum := 0.0
		for i := 0; i < n; i++ {
			accum += probs[i]
			if pick <= accum {
				selectedIndex = i
				break
			}
		}
		selected := stats[selectedIndex]
		mu.RUnlock()

		// --- 2. Execution Phase ---
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

		// --- 3. Reward & Update Phase ---
		reward := cfg.RewardFn(out, err, duration)

		mu.Lock()
		selected.Weight += cfg.LearningRate * (reward - selected.Weight)
		selected.Calls++
		mu.Unlock()

		return out, err
	}).
		Description(fmt.Sprintf("High-throughput RL router managing %d paths", len(candidates))).
		Build()
}
