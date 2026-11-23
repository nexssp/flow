package stream_ops_test

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/stream_ops"
)

func streamSource(items ...any) action.AnyStreamAction {
	return action.NewStream("test.source", func(_ context.Context, _ struct{}) (iter.Seq2[any, error], error) {
		return func(yield func(any, error) bool) {
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
		}, nil
	})
}

func TestStreamOps_BatchAndWindow(t *testing.T) {
	t.Parallel()

	bundle := stream_ops.Bundle(nil)
	ktest.RequireEqual(t, bundle.ID, "stream_ops")

	resolver, err := compiler.NewDynamicResolver(bundle.Libraries...)
	ktest.RequireNoError(t, err)

	batchOp, ok := resolver.Operator("stream.batch")
	ktest.RequireTrue(t, ok)

	built, err := batchOp.Build(map[string]any{"size": 2})
	ktest.RequireNoError(t, err)

	src := streamSource(1, 2, 3, 4, 5)
	anyStream, err := src.DoStreamAny(t.Context(), struct{}{})
	ktest.RequireNoError(t, err)

	batched, err := built.Apply(anyStream)
	ktest.RequireNoError(t, err)

	var batches [][]any
	batched(func(item any, itemErr error) bool {
		ktest.RequireNoError(t, itemErr)
		batch, ok := item.([]any)
		if !ok {
			t.Errorf("stream.batch emitted %T, want []any", item)
			return false
		}
		batches = append(batches, batch)
		return true
	})

	ktest.RequireEqual(t, len(batches), 3)
	ktest.RequireEqual(t, len(batches[0]), 2)
	ktest.RequireEqual(t, len(batches[1]), 2)
	ktest.RequireEqual(t, len(batches[2]), 1)
}

func TestStreamOps_ThrottleAndTake(t *testing.T) {
	t.Parallel()

	bundle := stream_ops.Bundle(nil)
	resolver, _ := compiler.NewDynamicResolver(bundle.Libraries...)

	takeOp, _ := resolver.Operator("stream.take")
	built, err := takeOp.Build(map[string]any{"count": 2})
	ktest.RequireNoError(t, err)

	src := streamSource("a", "b", "c", "d")
	anyStream, _ := src.DoStreamAny(t.Context(), struct{}{})
	taken, _ := built.Apply(anyStream)

	var items []any
	taken(func(item any, _ error) bool {
		items = append(items, item)
		return true
	})

	ktest.RequireEqual(t, len(items), 2)
	ktest.RequireEqual(t, items[0], "a")
	ktest.RequireEqual(t, items[1], "b")
}

func TestStreamOps_AllocationLimits(t *testing.T) {
	t.Parallel()
	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	cases := []struct {
		operator string
		config   map[string]any
	}{
		{"stream.batch", map[string]any{"size": 0}},
		{"stream.batch", map[string]any{"size": 1_000_001}},
		{"stream.window", map[string]any{"size": 1_000_001, "step": 1}},
		{"stream.batch", map[string]any{"size": 1, "max_bytes": int64(-1)}},
		{"stream.window", map[string]any{"size": 1, "step": 1, "max_bytes": int64(67_108_865)}},
		{"stream.collect", map[string]any{"limit": 1, "max_bytes": int64(-1)}},
		{"stream.take", map[string]any{}},
		{"stream.take", map[string]any{"count": 0}},
		{"stream.collect", map[string]any{"limit": 1_000_001}},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.operator, ".", "_")+"_invalid", func(t *testing.T) {
			t.Parallel()
			op, ok := resolver.Operator(tc.operator)
			ktest.RequireTrue(t, ok)
			_, err := op.Build(tc.config)
			if err == nil {
				t.Fatalf("Build(%v) unexpectedly succeeded", tc.config)
			}
		})
	}
}

func TestStreamOps_BufferByteBudgets(t *testing.T) {
	t.Parallel()
	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	cases := []struct {
		operator string
		config   map[string]any
	}{
		{"stream.batch", map[string]any{"size": 1, "max_bytes": 1}},
		{"stream.window", map[string]any{"size": 1, "step": 1, "max_bytes": 1}},
		{"stream.collect", map[string]any{"limit": 1, "max_bytes": 1}},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.operator, ".", "_")+"_over_budget", func(t *testing.T) {
			op, ok := resolver.Operator(tc.operator)
			ktest.RequireTrue(t, ok)
			built, err := op.Build(tc.config)
			ktest.RequireNoError(t, err)
			upstream, err := streamSource("too large").DoStreamAny(t.Context(), struct{}{})
			ktest.RequireNoError(t, err)
			buffered, err := built.Apply(upstream)
			ktest.RequireNoError(t, err)
			var observed error
			buffered(func(_ any, itemErr error) bool {
				if itemErr != nil {
					observed = itemErr
				}
				return true
			})
			if observed == nil {
				t.Fatal("buffered operator accepted an item over max_bytes")
			}
			if kind := xerr.KindFrom(observed); kind != xerr.KindValidation {
				t.Fatalf("buffered operator error kind = %s, want %s: %v", kind, xerr.KindValidation, observed)
			}
		})
	}

	collect, ok := resolver.Operator("stream.collect")
	ktest.RequireTrue(t, ok)
	built, err := collect.Build(map[string]any{"limit": 2, "max_bytes": 2048})
	ktest.RequireNoError(t, err)
	upstream, err := streamSource("small", []int{1, 2}).DoStreamAny(t.Context(), struct{}{})
	ktest.RequireNoError(t, err)
	buffered, err := built.Apply(upstream)
	ktest.RequireNoError(t, err)
	var got []any
	var observed error
	buffered(func(item any, itemErr error) bool {
		if itemErr != nil {
			observed = itemErr
			return true
		}
		var isSlice bool
		got, isSlice = item.([]any)
		if !isSlice {
			t.Errorf("stream.collect emitted %T, want []any", item)
		}
		return true
	})
	if observed != nil {
		t.Fatalf("stream.collect errored within max_bytes: %v", observed)
	}
	if len(got) != 2 || got[0] != "small" {
		t.Fatalf("collected values = %#v, want the two source values", got)
	}
	values, ok := got[1].([]int)
	if !ok || len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("collected typed slice = %#v, want []int{1, 2}", got[1])
	}
}

func TestStreamOps_ThrottleIntervalBoundaries(t *testing.T) {
	t.Parallel()
	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	op, ok := resolver.Operator("stream.throttle")
	ktest.RequireTrue(t, ok)
	for _, interval := range []int64{0, -1, 9_223_372_036_855} {
		if _, err := op.Build(map[string]any{"interval_ms": interval}); err == nil {
			t.Errorf("Build(interval_ms=%d) unexpectedly succeeded", interval)
		}
	}
	if _, err := op.Build(map[string]any{"interval_ms": int64(9_223_372_036_854)}); err != nil {
		t.Errorf("maximum representable interval rejected: %v", err)
	}
}

func TestStreamOps_BoundedOperatorsClassifyUnsupportedValues(t *testing.T) {
	t.Parallel()
	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	cases := []struct {
		operator string
		config   map[string]any
	}{
		{"stream.batch", map[string]any{"size": 1}},
		{"stream.collect", map[string]any{"limit": 1}},
		{"stream.window", map[string]any{"size": 1, "step": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.operator, func(t *testing.T) {
			t.Parallel()
			op, ok := resolver.Operator(tc.operator)
			ktest.RequireTrue(t, ok)
			built, err := op.Build(tc.config)
			ktest.RequireNoError(t, err)
			upstream, err := streamSource(make(chan int)).DoStreamAny(t.Context(), struct{}{})
			ktest.RequireNoError(t, err)
			buffered, err := built.Apply(upstream)
			ktest.RequireNoError(t, err)
			var observed error
			buffered(func(_ any, itemErr error) bool {
				if itemErr != nil {
					observed = itemErr
				}
				return true
			})
			if observed == nil {
				t.Fatal("bounded operator accepted an unsupported channel value")
			}
			if kind := xerr.KindFrom(observed); kind != xerr.KindValidation {
				t.Fatalf("bounded operator error kind = %s, want %s: %v", kind, xerr.KindValidation, observed)
			}
		})
	}
}
