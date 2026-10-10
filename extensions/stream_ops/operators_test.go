package stream_ops_test

import (
	"iter"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/stream_ops"
)

func streamSource(items ...any) action.AnyStreamAction {
	return action.NewStream("test.source", func(_ any, _ struct{}) (iter.Seq2[any, error], error) {
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

	resolver, err := core.NewDynamicResolver(bundle.Libraries...)
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
		batches = append(batches, item.([]any))
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
	resolver, _ := core.NewDynamicResolver(bundle.Libraries...)

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
