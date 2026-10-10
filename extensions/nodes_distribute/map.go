package nodes_distribute

import (
	"context"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"golang.org/x/sync/errgroup"

	"github.com/nexssp/flow/contracts"
)

const (
	defaultConcurrency = 4
	maxConcurrency     = 256
	maxItems           = 100_000
)

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

	// ─── Kernel Composition ──────────────────────────────────────────────
	// Compose target once with Kernel's native middleware. All goroutines
	// share the rate limiter, and each execution gets its own retry/timeout.
	builder := action.Dynamic(target)
	if req.TimeoutMs > 0 {
		builder = builder.Timeout(time.Duration(req.TimeoutMs) * time.Millisecond)
	}
	if req.Retries > 0 {
		builder = builder.Retry(req.Retries, action.ExponentialJitter(10*time.Millisecond, 2*time.Second))
	}
	if req.RateLimit > 0 {
		builder = builder.RateLimit(float64(req.RateLimit), max(req.RateLimit/10, 1))
	}
	execTarget := builder.Build()

	items := make([]Item, len(req.Items))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)

	for i, value := range req.Items {
		index, val := i, value
		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				items[index] = Item{Error: err.Error()}
				return err
			}

			output, err := action.InvokeAny(groupCtx, execTarget, val)
			if err != nil {
				items[index] = Item{Error: err.Error()}
				if req.FailFast {
					return err
				}
				return nil
			}

			items[index] = Item{OK: true, Result: output}
			return nil
		})
	}

	err := group.Wait()
	if err != nil && req.FailFast {
		return DistributeMapRes{}, err
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
