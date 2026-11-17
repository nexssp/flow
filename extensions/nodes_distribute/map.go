package nodes_distribute

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
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

func mapTimeoutDuration(timeoutMS int64) (time.Duration, error) {
	if timeoutMS < 0 || timeoutMS > int64(math.MaxInt64)/int64(time.Millisecond) {
		return 0, xerr.BadRequest("distribute.map: timeout_ms is outside the supported range")
	}
	return time.Duration(timeoutMS) * time.Millisecond, nil
}

// MapAction runs a compiled action over each item with bounded concurrency,
// preserving input order. It fails on the first action error, cancels sibling
// work, and waits for launched actions to finish before returning.
func MapAction(ctx context.Context, target action.AnyAction, items []any, concurrency int) ([]any, error) {
	if len(items) > maxItems {
		return nil, xerr.BadRequest(fmt.Sprintf("fanout: %d items exceeds limit of %d", len(items), maxItems))
	}
	if len(items) == 0 {
		return []any{}, nil
	}
	results, err := runFanOut(ctx, target, items, concurrency, true)
	if err != nil {
		return nil, err
	}
	values := make([]any, len(results))
	for i, result := range results {
		values[i] = result.Value
	}
	return values, nil
}

func runFanOut(
	ctx context.Context,
	target action.AnyAction,
	items []any,
	concurrency int,
	failFast bool,
) ([]action.AsyncResult[any], error) {
	if isNilAction(target) {
		return nil, xerr.Internal("fanout: target action is nil")
	}
	if len(items) == 0 {
		return nil, nil
	}
	concurrency = normalizeFanoutConcurrency(concurrency)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var firstErr error
	var firstErrOnce sync.Once

	builder := action.Dynamic(target)
	builder = builder.HookBefore(func(itemCtx context.Context, _ any, _ *action.Meta) (context.Context, error) {
		if err := itemCtx.Err(); err != nil {
			return itemCtx, &fanoutNotStartedError{cause: err}
		}
		return itemCtx, nil
	})
	if failFast {
		builder = builder.HookAfter(func(_ context.Context, _, _ any, err error, _ *action.Meta) {
			if err == nil {
				return
			}
			if notStarted, ok := errors.AsType[*fanoutNotStartedError](err); ok && notStarted != nil {
				return
			}
			firstErrOnce.Do(func() {
				firstErr = err
				cancel()
			})
		})
	}

	results := action.FanOut(runCtx, builder.Build(), items, concurrency)
	if failFast {
		if firstErr != nil {
			return nil, firstErr
		}
		for _, result := range results {
			if result.Err != nil {
				return nil, normalizeFanoutError(result.Err)
			}
		}
	}
	return results, nil
}

func isNilAction(target action.AnyAction) bool {
	if target == nil {
		return true
	}
	value := reflect.ValueOf(target)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	case reflect.Array, reflect.Bool, reflect.Complex64, reflect.Complex128, reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.String, reflect.Struct,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Invalid:
		return false
	}
	return false
}

func normalizeFanoutConcurrency(concurrency int) int {
	if concurrency <= 0 {
		return defaultConcurrency
	}
	if concurrency > maxConcurrency {
		return maxConcurrency
	}
	return concurrency
}

var DistributeMap = action.New("distribute.map", func(ctx context.Context, req DistributeMapReq) (DistributeMapRes, error) {
	if req.Action == "" {
		return DistributeMapRes{}, xerr.BadRequest("distribute.map: action is required")
	}
	timeout, err := mapTimeoutDuration(req.TimeoutMs)
	if err != nil {
		return DistributeMapRes{}, err
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

	// Compose target once with Kernel's native middleware. All fan-out workers
	// share the rate limiter, and each execution gets its own retry/timeout.
	builder := action.Dynamic(target)
	if timeout > 0 {
		builder = builder.Timeout(timeout)
	}
	if req.Retries > 0 {
		builder = builder.Retry(req.Retries, action.ExponentialJitter(10*time.Millisecond, 2*time.Second))
	}
	if req.RateLimit > 0 {
		builder = builder.RateLimit(float64(req.RateLimit), max(req.RateLimit/10, 1))
	}

	results, err := runFanOut(ctx, builder.Build(), req.Items, req.Concurrency, req.FailFast)
	if err != nil {
		return DistributeMapRes{}, err
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
