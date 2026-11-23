package stream_ops_test

import (
	"reflect"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/stream_ops"
)

type reviewUnexportedScalarFields struct {
	ID int
}

type reviewItemWithUnexportedScalarEmbed struct {
	reviewUnexportedScalarFields
}

func TestStreamOps_WindowSlidesAndSkipsCorrectly(t *testing.T) {
	t.Parallel()
	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	cases := []struct {
		name string
		size int
		step int
		in   []any
		want [][]any
	}{
		{
			name: "overlapping windows",
			size: 3,
			step: 2,
			in:   []any{1, 2, 3, 4, 5},
			want: [][]any{{1, 2, 3}, {3, 4, 5}, {5}},
		},
		{
			name: "skipped inputs and trailing window",
			size: 2,
			step: 3,
			in:   []any{1, 2, 3, 4, 5, 6, 7},
			want: [][]any{{1, 2}, {4, 5}, {7}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op, ok := resolver.Operator("stream.window")
			ktest.RequireTrue(t, ok)
			built, err := op.Build(map[string]any{"size": tc.size, "step": tc.step})
			ktest.RequireNoError(t, err)
			upstream, err := streamSource(tc.in...).DoStreamAny(t.Context(), struct{}{})
			ktest.RequireNoError(t, err)
			buffered, err := built.Apply(upstream)
			ktest.RequireNoError(t, err)
			var got [][]any
			var observed error
			buffered(func(item any, itemErr error) bool {
				if itemErr != nil {
					observed = itemErr
					return false
				}
				window, ok := item.([]any)
				if !ok {
					t.Errorf("stream.window emitted %T, want []any", item)
					return false
				}
				got = append(got, window)
				return true
			})
			if observed != nil {
				t.Fatalf("stream.window error: %v", observed)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("stream.window results = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestStreamOps_ClassifiesUnexportedEmbeddedScalarAsValidation(t *testing.T) {
	t.Parallel()
	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	op, ok := resolver.Operator("stream.collect")
	ktest.RequireTrue(t, ok)
	built, err := op.Build(map[string]any{"limit": 1})
	ktest.RequireNoError(t, err)
	item := reviewItemWithUnexportedScalarEmbed{
		reviewUnexportedScalarFields{ID: 123},
	}
	upstream, err := streamSource(item).DoStreamAny(t.Context(), struct{}{})
	ktest.RequireNoError(t, err)
	buffered, err := built.Apply(upstream)
	ktest.RequireNoError(t, err)
	var observed error
	buffered(func(_ any, itemErr error) bool {
		observed = itemErr
		return itemErr == nil
	})
	if observed == nil {
		t.Fatal("stream.collect accepted an unexported embedded struct with exported fields")
	}
	if kind := xerr.KindFrom(observed); kind != xerr.KindValidation {
		t.Fatalf("embedded struct error kind = %s, want %s: %v", kind, xerr.KindValidation, observed)
	}
}
