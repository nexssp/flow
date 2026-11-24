package nodes_distribute

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/contracts"
)

type mapTestResolver struct {
	target action.AnyAction
	calls  *atomic.Int32
}

func (r mapTestResolver) Action(name string) (action.AnyAction, bool) {
	if r.calls != nil {
		r.calls.Add(1)
	}
	if name != "test.worker" {
		return nil, false
	}
	return r.target, true
}

func TestDistributeMapUsesKernelFanOutAndPreservesResultOrder(t *testing.T) {
	firstStarted := make(chan struct{})
	secondDone := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	var secondDoneOnce sync.Once

	target := action.New("test.worker", func(_ context.Context, input any) (any, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for observed := maximum.Load(); current > observed; observed = maximum.Load() {
			if maximum.CompareAndSwap(observed, current) {
				break
			}
		}

		item, ok := input.(int)
		if !ok {
			return nil, errors.New("unexpected input")
		}
		switch item {
		case 1:
			close(firstStarted)
			<-secondDone
			return "first", nil
		case 2:
			<-firstStarted
			secondDoneOnce.Do(func() { close(secondDone) })
			return nil, errors.New("item two failed")
		case 3:
			return "third", nil
		default:
			return nil, errors.New("unexpected input")
		}
	}).Build()
	ctx := contracts.WithActionResolver(t.Context(), mapTestResolver{target: target})

	got, err := DistributeMap.Do(ctx, DistributeMapReq{
		Action:      "test.worker",
		Concurrency: 2,
		Items:       []any{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("DistributeMap.Do: %v", err)
	}
	if got.Succeeded != 2 || got.Failed != 1 {
		t.Fatalf("succeeded/failed = %d/%d, want 2/1", got.Succeeded, got.Failed)
	}
	if len(got.Items) != 3 {
		t.Fatalf("items length = %d, want 3", len(got.Items))
	}
	if !got.Items[0].OK || got.Items[0].Result != "first" {
		t.Fatalf("item 0 = %#v, want successful first result", got.Items[0])
	}
	if got.Items[1].OK || !strings.Contains(got.Items[1].Error, "item two failed") {
		t.Fatalf("item 1 = %#v, want item error", got.Items[1])
	}
	if strings.Contains(got.Items[1].Error, "distribute.map.item") {
		t.Fatalf("item error gained an adapter wrapper prefix: %q", got.Items[1].Error)
	}
	if !got.Items[2].OK || got.Items[2].Result != "third" {
		t.Fatalf("item 2 = %#v, want successful third result", got.Items[2])
	}
	if maximum.Load() > 2 {
		t.Fatalf("maximum concurrent workers = %d, want at most 2", maximum.Load())
	}
}

func TestMapTimeoutDurationChecksLimits(t *testing.T) {
	maxTimeoutMS := int64(math.MaxInt64) / int64(time.Millisecond)
	if got, err := mapTimeoutDuration(0); err != nil || got != 0 {
		t.Fatalf("zero timeout = %s, %v; want 0, nil", got, err)
	}
	if got, err := mapTimeoutDuration(maxTimeoutMS); err != nil || got <= 0 {
		t.Fatalf("maximum timeout = %s, %v; want positive duration, nil", got, err)
	}
	for _, timeout := range []int64{-1, maxTimeoutMS + 1} {
		_, err := mapTimeoutDuration(timeout)
		if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
			t.Fatalf("timeout %d error kind = %s, want %s: %v", timeout, got, xerr.KindBadRequest, err)
		}
	}
}

func TestNormalizeFanoutConcurrency(t *testing.T) {
	for _, tc := range []struct {
		input int
		want  int
	}{
		{input: -1, want: defaultConcurrency},
		{input: 0, want: defaultConcurrency},
		{input: 2, want: 2},
		{input: maxConcurrency, want: maxConcurrency},
		{input: maxConcurrency + 1, want: maxConcurrency},
		{input: 999, want: maxConcurrency},
	} {
		if got := normalizeFanoutConcurrency(tc.input); got != tc.want {
			t.Errorf("normalizeFanoutConcurrency(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestMapActionRejectsOverLimitWithCounts(t *testing.T) {
	items := make([]any, maxItems+1)
	_, err := MapAction(t.Context(), nil, items, 1)
	if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
		t.Fatalf("MapAction error kind = %s, want %s: %v", got, xerr.KindBadRequest, err)
	}
	want := "fanout: 100001 items exceeds limit of 100000"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("MapAction error = %v, want message containing %q", err, want)
	}
}

func TestMapActionRejectsTypedNilTarget(t *testing.T) {
	var target *action.BuiltAction[any, any]
	_, err := MapAction(t.Context(), target, []any{1}, 1)
	if got := xerr.KindFrom(err); got != xerr.KindInternal {
		t.Fatalf("MapAction error kind = %s, want %s: %v", got, xerr.KindInternal, err)
	}
	if !strings.Contains(err.Error(), "fanout: target action is nil") {
		t.Fatalf("MapAction error = %v, want nil-target diagnostic", err)
	}
}

func TestDistributeMapRejectsInvalidTimeoutBeforeEmptyResult(t *testing.T) {
	maxTimeoutMS := int64(math.MaxInt64) / int64(time.Millisecond)
	for _, timeout := range []int64{-1, maxTimeoutMS + 1} {
		_, err := DistributeMap.Do(t.Context(), DistributeMapReq{
			Action:    "test.worker",
			TimeoutMs: timeout,
		})
		if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
			t.Fatalf("timeout %d error kind = %s, want %s: %v", timeout, got, xerr.KindBadRequest, err)
		}
	}
}

func TestDistributeMapRejectsNegativeRetryAndRateLimitBeforeResolution(t *testing.T) {
	cases := []struct {
		name string
		req  DistributeMapReq
	}{
		{name: "retries", req: DistributeMapReq{Action: "test.worker", Items: []any{1}, Retries: -1}},
		{name: "rate_limit", req: DistributeMapReq{Action: "test.worker", Items: []any{1}, RateLimit: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resolutions, calls atomic.Int32
			target := action.New("test.worker", func(context.Context, any) (any, error) {
				calls.Add(1)
				return nil, nil
			}).Build()
			ctx := contracts.WithActionResolver(t.Context(), mapTestResolver{target: target, calls: &resolutions})

			_, err := DistributeMap.Do(ctx, tc.req)
			if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
				t.Fatalf("error kind = %s, want %s: %v", got, xerr.KindBadRequest, err)
			}
			if resolutions.Load() != 0 {
				t.Fatalf("resolver calls = %d, want 0", resolutions.Load())
			}
			if calls.Load() != 0 {
				t.Fatalf("worker calls = %d, want 0", calls.Load())
			}
		})
	}
}

func TestDistributeMapZeroRetriesAndRateLimitKeepDefaults(t *testing.T) {
	var calls atomic.Int32
	target := action.New("test.worker", func(context.Context, any) (any, error) {
		calls.Add(1)
		return nil, errors.New("one attempt only")
	}).Build()
	ctx := contracts.WithActionResolver(t.Context(), mapTestResolver{target: target})
	got, err := DistributeMap.Do(ctx, DistributeMapReq{
		Action:    "test.worker",
		Items:     []any{1},
		Retries:   0,
		RateLimit: 0,
	})
	if err != nil {
		t.Fatalf("DistributeMap.Do: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].OK {
		t.Fatalf("result items = %#v, want one failed item", got.Items)
	}
	if calls.Load() != 1 {
		t.Fatalf("worker calls = %d, want 1 with zero retries", calls.Load())
	}
}

func TestDistributeMapAppliesTimeoutMS(t *testing.T) {
	observedDeadline := make(chan time.Duration, 1)
	target := action.New("test.worker", func(ctx context.Context, _ any) (any, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, errors.New("worker context has no deadline")
		}
		observedDeadline <- time.Until(deadline)
		<-ctx.Done()
		return nil, ctx.Err()
	}).Build()

	parentCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx := contracts.WithActionResolver(parentCtx, mapTestResolver{target: target})
	got, err := DistributeMap.Do(ctx, DistributeMapReq{
		Action:    "test.worker",
		Items:     []any{1},
		TimeoutMs: 100,
	})
	if err != nil {
		t.Fatalf("DistributeMap.Do: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].OK {
		t.Fatalf("map result = %#v, want one failed item", got.Items)
	}
	if remaining := <-observedDeadline; remaining <= 0 || remaining > 100*time.Millisecond {
		t.Fatalf("worker deadline remaining = %s, want (0, 100ms]", remaining)
	}
}

func TestDistributeMapFailFastCancelsInFlightSibling(t *testing.T) {
	startedSibling := make(chan struct{})
	failure := errors.New("first item failed")
	var calls atomic.Int32

	target := action.New("test.worker", func(ctx context.Context, input any) (any, error) {
		calls.Add(1)
		item, ok := input.(int)
		if !ok {
			return nil, errors.New("unexpected input")
		}
		switch item {
		case 0:
			<-startedSibling
			return nil, failure
		case 1:
			close(startedSibling)
			<-ctx.Done()
			return nil, ctx.Err()
		case 2:
			return "must not run", nil
		default:
			return nil, errors.New("unexpected input")
		}
	}).Build()
	ctx := contracts.WithActionResolver(t.Context(), mapTestResolver{target: target})

	_, err := DistributeMap.Do(ctx, DistributeMapReq{
		Action:      "test.worker",
		Concurrency: 2,
		Items:       []any{0, 1, 2},
		FailFast:    true,
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want original first failure", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("worker calls = %d, want only the two already in flight", got)
	}
}

func TestDistributeMapDoesNotInvokeWorkersAfterCancellation(t *testing.T) {
	var calls atomic.Int32
	target := action.New("test.worker", func(context.Context, any) (any, error) {
		calls.Add(1)
		return "unexpected", nil
	}).Build()

	for _, failFast := range []bool{false, true} {
		t.Run(map[bool]string{false: "collect_errors", true: "fail_fast"}[failFast], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			ctx = contracts.WithActionResolver(ctx, mapTestResolver{target: target})

			got, err := DistributeMap.Do(ctx, DistributeMapReq{
				Action:      "test.worker",
				Concurrency: 1,
				Items:       []any{1, 2},
				FailFast:    failFast,
			})
			if failFast {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want context.Canceled", err)
				}
				if len(got.Items) != 0 {
					t.Fatalf("fail-fast result = %#v, want empty result", got)
				}
			} else {
				if err != nil {
					t.Fatalf("DistributeMap.Do: %v", err)
				}
				if got.Succeeded != 0 || got.Failed != 2 {
					t.Fatalf("succeeded/failed = %d/%d, want 0/2", got.Succeeded, got.Failed)
				}
				for i, item := range got.Items {
					if item.OK || item.Error != context.Canceled.Error() {
						t.Fatalf("item %d = %#v, want raw context cancellation", i, item)
					}
				}
			}
			if calls.Load() != 0 {
				t.Fatalf("worker calls = %d, want 0 after cancellation", calls.Load())
			}
		})
	}
}
