package nodes_distribute

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler/contracts"
)

type mapTestResolver struct {
	target action.AnyAction
}

func (r mapTestResolver) Action(name string) (action.AnyAction, bool) {
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
