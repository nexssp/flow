package nodes_distribute

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"golang.org/x/sync/errgroup"

	"github.com/nexssp/flow/contracts"
)

const (
	defaultConcurrency = 4
	maxConcurrency     = 256
	maxItems           = 10_000
)

// DistributeMapReq carries the fan-out configuration.
type DistributeMapReq struct {
	Action      string `json:"action"      validate:"required"`
	Concurrency int    `json:"concurrency,omitempty"`
	Items       []any  `json:"items"       validate:"required"`
}

// DistributeMapRes is the ordered result; index i corresponds to
// input item i regardless of completion order.
type DistributeMapRes struct {
	Items     []Item `json:"items"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
}

// DistributeMap invokes Action once per input item with bounded
// concurrency, preserving input order in the output.
var DistributeMap = action.New("distribute.map", func(ctx context.Context, req DistributeMapReq) (DistributeMapRes, error) {
	if req.Action == "" {
		return DistributeMapRes{}, xerr.BadRequest("distribute.map: action is required")
	}
	if len(req.Items) == 0 {
		return DistributeMapRes{}, nil
	}
	if len(req.Items) > maxItems {
		return DistributeMapRes{}, xerr.BadRequest("distribute.map: items exceeds limit")
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

	items := make([]Item, len(req.Items))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)

	for i, value := range req.Items {
		index, val := i, value
		group.Go(func() error {
			// Only abort the execution if the parent context was canceled (e.g. timeout)
			if err := groupCtx.Err(); err != nil {
				items[index] = Item{Error: err.Error()}
				return err
			}

			output, err := invokeSafely(groupCtx, target, val)
			if err != nil {
				items[index] = Item{Error: err.Error()}
				return nil // Return nil so the errgroup continues processing other items!
			}

			items[index] = Item{OK: true, Result: output}
			return nil
		})
	}

	// Wait only returns an error if groupCtx.Err() triggered an abort
	if err := group.Wait(); err != nil {
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
}).Description("Fan out one action over many items with bounded concurrency").
	Tag("distribute", "fanout").
	Build()

// invokeSafely isolates a panic in the target action and returns it as
// an error, so one bad item cannot crash the fan-out.
func invokeSafely(ctx context.Context, target action.AnyAction, input any) (output any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			output = nil
			err = xerr.PanicRecovery(recovered)
		}
	}()
	return action.InvokeAny(ctx, target, input)
}
