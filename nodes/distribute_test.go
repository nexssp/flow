package nodes_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

func TestDistributeMap_HappyPathPreservesOrder(t *testing.T) {
	reg := flow.NewRegistry()
	reg.Register("echo", action.New("echo",
		func(_ context.Context, in int) (int, error) {
			// Sleep decreases with value so slower items are scheduled
			// first if the implementation does not preserve order.
			time.Sleep(time.Duration(50-in) * time.Millisecond)

			return in, nil
		},
	).Build())

	res := mustInvoke(t, nodes.NewDistributeMapAction(reg), nodes.DistributeMapReq{
		Action:      "echo",
		Concurrency: 8,
		Items:       []any{1, 2, 3, 4, 5},
	}).(nodes.DistributeMapRes)

	if res.Succeeded != 5 || res.Failed != 0 {
		t.Fatalf("unexpected counts: %+v", res)
	}

	for i, item := range res.Items {
		want := i + 1
		if !item.OK || item.Result != want {
			t.Fatalf("slot %d wrong: %+v", i, item)
		}
	}
}

func TestDistributeMap_ConcurrencyIsBounded(t *testing.T) {
	reg := flow.NewRegistry()

	var inFlight, peak atomic.Int64

	reg.Register("slow", action.New("slow",
		func(_ context.Context, _ int) (int, error) {
			n := inFlight.Add(1)

			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}

			time.Sleep(30 * time.Millisecond)
			inFlight.Add(-1)

			return 0, nil
		},
	).Build())

	res := mustInvoke(t, nodes.NewDistributeMapAction(reg), nodes.DistributeMapReq{
		Action:      "slow",
		Concurrency: 3,
		Items:       []any{1, 2, 3, 4, 5, 6, 7, 8, 9},
	}).(nodes.DistributeMapRes)

	if res.Succeeded != 9 {
		t.Fatalf("expected 9 successes: %+v", res)
	}

	if peak.Load() > 3 {
		t.Fatalf("concurrency exceeded limit: peak=%d", peak.Load())
	}
}

func TestDistributeMap_FailureIsolatedPerSlot(t *testing.T) {
	reg := flow.NewRegistry()
	reg.Register("maybe", action.New("maybe",
		func(_ context.Context, in int) (int, error) {
			if in == 3 {
				return 0, fmt.Errorf("simulated failure")
			}

			return in, nil
		},
	).Build())

	res := mustInvoke(t, nodes.NewDistributeMapAction(reg), nodes.DistributeMapReq{
		Action:      "maybe",
		Concurrency: 4,
		Items:       []any{1, 2, 3, 4, 5},
	}).(nodes.DistributeMapRes)

	if res.Succeeded != 4 || res.Failed != 1 {
		t.Fatalf("expected 4/1, got %+v", res)
	}

	if res.Items[2].OK || res.Items[2].Error == "" {
		t.Fatalf("slot 2 must record the failure: %+v", res.Items[2])
	}
	// Sibling slots must still have succeeded.
	if !res.Items[0].OK || !res.Items[4].OK {
		t.Fatalf("failure leaked to siblings: %+v", res.Items)
	}
}

func TestDistributeMap_PanicIsContainedPerSlot(t *testing.T) {
	reg := flow.NewRegistry()
	reg.Register("boom", action.New("boom",
		func(_ context.Context, in int) (int, error) {
			if in == 2 {
				panic("kaboom")
			}

			return in, nil
		},
	).Build())

	res := mustInvoke(t, nodes.NewDistributeMapAction(reg), nodes.DistributeMapReq{
		Action: "boom",
		Items:  []any{1, 2, 3},
	}).(nodes.DistributeMapRes)

	if res.Failed != 1 || res.Succeeded != 2 {
		t.Fatalf("expected 2/1, got %+v", res)
	}

	if !strings.Contains(res.Items[1].Error, "panic") {
		t.Fatalf("expected panic marker, got %q", res.Items[1].Error)
	}
}

func TestDistributeMap_EmptyItemsIsNoop(t *testing.T) {
	reg := flow.NewRegistry()

	res := mustInvoke(t, nodes.NewDistributeMapAction(reg), nodes.DistributeMapReq{
		Action: "anything",
		Items:  []any{},
	}).(nodes.DistributeMapRes)
	if res.Succeeded != 0 || res.Failed != 0 || len(res.Items) != 0 {
		t.Fatalf("expected empty result, got %+v", res)
	}
}

func TestDistributeMap_ItemsExceedLimit(t *testing.T) {
	reg := flow.NewRegistry()
	items := make([]any, nodes.DistributeMaxItemsForTest()+1)

	_, err := action.InvokeAny(context.Background(), nodes.NewDistributeMapAction(reg),
		nodes.DistributeMapReq{Action: "x", Items: items})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestDistributeMap_MissingAction(t *testing.T) {
	reg := flow.NewRegistry()

	_, err := action.InvokeAny(context.Background(), nodes.NewDistributeMapAction(reg),
		nodes.DistributeMapReq{Action: "missing", Items: []any{1}})
	if err == nil {
		t.Fatal("expected NotFound")
	}
}

func TestDistributeMap_NilRegistry(t *testing.T) {
	_, err := action.InvokeAny(context.Background(), nodes.NewDistributeMapAction(nil),
		nodes.DistributeMapReq{Action: "x", Items: []any{1}})
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestDistributeMap_ContextCanceledStops(t *testing.T) {
	reg := flow.NewRegistry()
	reg.Register("slow", action.New("slow",
		func(ctx context.Context, _ int) (int, error) {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return 0, nil
			}
		},
	).Build())

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	res, err := action.InvokeAny(ctx, nodes.NewDistributeMapAction(reg),
		nodes.DistributeMapReq{Action: "slow", Concurrency: 2, Items: []any{1, 2, 3, 4}})
	if err != nil {
		t.Fatal(err)
	}
	// Slots that did not get to run must carry the context error.
	failed := res.(nodes.DistributeMapRes).Failed
	if failed == 0 {
		t.Fatalf("expected some failures after cancel: %+v", res)
	}
}

// ── reduce ─────────────────────────────────────────────────────────────────

func TestDistributeReduce_AllStrategies(t *testing.T) {
	items := []nodes.DistributeItem{
		{OK: true, Result: 1},
		{OK: false, Error: "nope"},
		{OK: true, Result: 3},
	}
	reg := flow.NewRegistry()

	cases := []struct {
		strategy string
		assert   func(t *testing.T, r nodes.DistributeReduceRes)
	}{
		{"collect", func(t *testing.T, r nodes.DistributeReduceRes) {
			if len(r.Results) != 2 || r.Results[0] != 1 || r.Results[1] != 3 {
				t.Fatalf("collect wrong: %+v", r)
			}
		}},
		{"all_pass", func(t *testing.T, r nodes.DistributeReduceRes) {
			if r.AllPass {
				t.Fatal("all_pass should be false")
			}
		}},
		{"any_pass", func(t *testing.T, r nodes.DistributeReduceRes) {
			if !r.AnyPass {
				t.Fatal("any_pass should be true")
			}
		}},
		{"first_success", func(t *testing.T, r nodes.DistributeReduceRes) {
			if r.Result != 1 {
				t.Fatalf("first_success wrong: %+v", r)
			}
		}},
		{"count", func(t *testing.T, r nodes.DistributeReduceRes) {
			if r.Succeeded != 2 || r.Failed != 1 {
				t.Fatalf("count wrong: %+v", r)
			}
		}},
	}

	for _, c := range cases {
		t.Run(c.strategy, func(t *testing.T) {
			res := mustInvoke(t, nodes.NewDistributeReduceAction(), nodes.DistributeReduceReq{
				Strategy: c.strategy,
				Items:    items,
			}).(nodes.DistributeReduceRes)
			c.assert(t, res)
		})
	}

	_ = reg
}

func TestDistributeReduce_FirstSuccessNoneSucceeds(t *testing.T) {
	res, err := action.InvokeAny(context.Background(), nodes.NewDistributeReduceAction(),
		nodes.DistributeReduceReq{
			Strategy: "first_success",
			Items:    []nodes.DistributeItem{{OK: false, Error: "x"}},
		})
	if err == nil {
		t.Fatalf("expected error, got %+v", res)
	}
}

func TestDistributeReduce_AllPassOnEmptyIsTrue(t *testing.T) {
	res := mustInvoke(t, nodes.NewDistributeReduceAction(),
		nodes.DistributeReduceReq{Strategy: "all_pass", Items: []nodes.DistributeItem{}}).(nodes.DistributeReduceRes)
	if !res.AllPass {
		t.Fatalf("all_pass on empty must be true: %+v", res)
	}
}

func TestDistributeReduce_UnknownStrategy(t *testing.T) {
	_, err := action.InvokeAny(context.Background(), nodes.NewDistributeReduceAction(),
		nodes.DistributeReduceReq{
			Strategy: "nonsense",
			Items:    []nodes.DistributeItem{{OK: true}},
		})
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}
