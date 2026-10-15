package nodes_distribute

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/contracts"
)

const (
	defaultConcurrency = 4
	maxConcurrency     = 256
	maxItems           = 100_000
)

type fanoutNotStartedError struct {
	cause error
}

func (e *fanoutNotStartedError) Error() string { return e.cause.Error() }
func (e *fanoutNotStartedError) Unwrap() error { return e.cause }

func normalizeFanoutError(err error) error {
	if notStarted, ok := errors.AsType[*fanoutNotStartedError](err); ok {
		return notStarted.cause
	}
	return err
}

type DistributeMapReq struct {
	Action      string `json:"action"                 validate:"required"`
	Concurrency int    `json:"concurrency,omitempty"`
	Items       []any  `json:"items"                  validate:"required"`
	RateLimit   int    `json:"rate_limit,omitempty"`
	TimeoutMs   int64  `json:"timeout_ms,omitempty"`
	Retries     int    `json:"retries,omitempty"`
	FailFast    bool   `json:"fail_fast,omitempty"`
}

type DistributeMapRes struct {
	Items     []Item `json:"items"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
}

var DistributeMap = action.New("distribute.map", func(ctx context.Context, req DistributeMapReq) (DistributeMapRes, error) {
	if req.Action == "" {
		return DistributeMapRes{}, xerr.BadRequest("distribute.map: action is required")
	}
	if len(req.Items) == 0 {
		return DistributeMapRes{}, nil
	}
	if len(req.Items) > maxItems {
		return DistributeMapRes{}, xerr.BadRequest("distribute.map: items count exceeds limit")
	}

	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return DistributeMapRes{}, xerr.Internal("distribute.map: no action resolver in context")
	}

	target, ok := resolver.Action(req.Action)
	if !ok {
		return DistributeMapRes{}, xerr.NotFound("distribute.map: action not found: " + req.Action)
	}

	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	if concurrency > maxConcurrency {
		concurrency = maxConcurrency
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var firstErr error
	var firstErrOnce sync.Once

	// Compose target once with Kernel's native middleware. All goroutines
	// share the rate limiter, and each execution gets its own retry/timeout.
	builder := action.Dynamic(target)
	builder = builder.HookBefore(func(itemCtx context.Context, _ any, _ *action.Meta) (context.Context, error) {
		if err := itemCtx.Err(); err != nil {
			return itemCtx, &fanoutNotStartedError{cause: err}
		}
		return itemCtx, nil
	})
	if req.TimeoutMs > 0 {
		builder = builder.Timeout(time.Duration(req.TimeoutMs) * time.Millisecond)
	}
	if req.Retries > 0 {
		builder = builder.Retry(req.Retries, action.ExponentialJitter(10*time.Millisecond, 2*time.Second))
	}
	if req.RateLimit > 0 {
		builder = builder.RateLimit(float64(req.RateLimit), max(req.RateLimit/10, 1))
	}
	if req.FailFast {
		builder = builder.HookAfter(func(_ context.Context, _, _ any, err error, _ *action.Meta) {
			if err != nil {
				if notStarted, ok := errors.AsType[*fanoutNotStartedError](err); ok && notStarted != nil {
					return
				}
				firstErrOnce.Do(func() {
					firstErr = err
					cancel()
				})
			}
		})
	}
	execTarget := builder.Build()

	results := action.FanOut(runCtx, execTarget, req.Items, concurrency)
	if req.FailFast {
		if firstErr != nil {
			return DistributeMapRes{}, firstErr
		}
		for _, result := range results {
			if result.Err != nil {
				return DistributeMapRes{}, normalizeFanoutError(result.Err)
			}
		}
	}

	items := make([]Item, len(results))
	for i, result := range results {
		if result.Err != nil {
			items[i] = Item{Error: normalizeFanoutError(result.Err).Error()}
			continue
		}
		items[i] = Item{OK: true, Result: result.Value}
	}

	result := DistributeMapRes{Items: items}
	for i := range items {
		if items[i].OK {
			result.Succeeded++
		} else {
			result.Failed++
		}
	}
	return result, nil
}).Description("Fan out one action over many items with Kernel rate-limiting, retry, and timeout").
	Tag("distribute", "fanout").
	Build()
