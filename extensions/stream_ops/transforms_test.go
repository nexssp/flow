package stream_ops_test

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/stream"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/stream_ops"
)

func TestStreamTransformsPreserveOrderAndUpstreamErrors(t *testing.T) {
	failure := errors.New("upstream failure")
	tests := []struct {
		name   string
		config map[string]any
		want   []any
	}{
		{name: "stream.map", config: map[string]any{"expression": "item + item"}, want: []any{"aa"}},
		{name: "stream.filter", config: map[string]any{"expression": "true"}, want: []any{"a"}},
		{name: "stream.flatMap", config: map[string]any{"expression": "[item, item]"}, want: []any{"a", "a"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operator := buildStreamOperator(t, test.name, test.config)
			afterError := false
			upstream := iter.Seq2[any, error](func(yield func(any, error) bool) {
				if !yield("a", nil) {
					return
				}
				if !yield(nil, failure) {
					return
				}
				afterError = true
				yield("after", nil)
			})
			output := applyStreamOperator(t, operator, upstream)

			var values []any
			var gotErrors []error
			output(func(value any, err error) bool {
				if err != nil {
					gotErrors = append(gotErrors, err)
					return true
				}
				values = append(values, value)
				return true
			})

			assertStreamValues(t, values, test.want)
			if len(gotErrors) != 1 || !errors.Is(gotErrors[0], failure) {
				t.Fatalf("stream errors = %v, want the original upstream error", gotErrors)
			}
			if afterError {
				t.Fatal("operator continued reading upstream after its error")
			}
		})
	}
}

func TestStreamTransformsPropagateContextCancellation(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]any
		wantFirst any
	}{
		{name: "stream.map", config: map[string]any{"expression": "item + 1"}, wantFirst: 2},
		{name: "stream.filter", config: map[string]any{"expression": "true"}, wantFirst: 1},
		{name: "stream.flatMap", config: map[string]any{"expression": "[item]"}, wantFirst: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			upstream := stream.WithContext[any](ctx)(testStream(1, 2, 3))
			output := applyStreamOperator(t, buildStreamOperator(t, test.name, test.config), upstream)

			var values []any
			var gotErrors []error
			output(func(value any, err error) bool {
				if err != nil {
					gotErrors = append(gotErrors, err)
					return true
				}
				values = append(values, value)
				cancel()
				return true
			})

			assertStreamValues(t, values, []any{test.wantFirst})
			if len(gotErrors) != 1 || !errors.Is(gotErrors[0], context.Canceled) {
				t.Fatalf("stream errors = %v, want context.Canceled", gotErrors)
			}
		})
	}
}

func TestStreamTransformsHonorDownstreamBackpressure(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
	}{
		{name: "stream.map", config: map[string]any{"expression": "item + 1"}},
		{name: "stream.filter", config: map[string]any{"expression": "true"}},
		{name: "stream.flatMap", config: map[string]any{"expression": "[item, item]"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			seen := 0
			upstream := iter.Seq2[any, error](func(yield func(any, error) bool) {
				for _, value := range []any{1, 2, 3} {
					seen++
					if !yield(value, nil) {
						return
					}
				}
			})
			output := applyStreamOperator(t, buildStreamOperator(t, test.name, test.config), upstream)
			outputs := 0
			output(func(_ any, err error) bool {
				if err != nil {
					t.Errorf("stream emitted error: %v", err)
				}
				outputs++
				return false
			})

			if outputs != 1 || seen != 1 {
				t.Fatalf("outputs/upstream reads = %d/%d, want 1/1", outputs, seen)
			}
		})
	}
}

func TestStreamTransformRejectsInvalidResultShape(t *testing.T) {
	tests := []struct {
		name       string
		wantErrMsg string
	}{
		{name: "stream.filter", wantErrMsg: "expression must return a boolean"},
		{name: "stream.flatMap", wantErrMsg: "expression must return an array"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := applyStreamOperator(
				t,
				buildStreamOperator(t, test.name, map[string]any{"expression": "item"}),
				testStream("not the expected result shape"),
			)
			var gotError error
			output(func(_ any, err error) bool {
				gotError = err
				return true
			})
			if gotError == nil || !strings.Contains(gotError.Error(), test.wantErrMsg) {
				t.Fatalf("error = %v, want it to contain %q", gotError, test.wantErrMsg)
			}
		})
	}
}

func buildStreamOperator(t *testing.T, name string, config map[string]any) action.StreamOperator {
	t.Helper()
	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	if err != nil {
		t.Fatalf("NewDynamicResolver: %v", err)
	}
	named, ok := resolver.Operator(name)
	if !ok {
		t.Fatalf("operator %q was not registered", name)
	}
	operator, err := named.Build(config)
	if err != nil {
		t.Fatalf("build operator %q: %v", name, err)
	}
	return operator
}

func applyStreamOperator(t *testing.T, operator action.StreamOperator, upstream iter.Seq2[any, error]) action.AnyStream {
	t.Helper()
	output, err := operator.Apply(action.AnyStream(upstream))
	if err != nil {
		t.Fatalf("apply operator %q: %v", operator.Name(), err)
	}
	return output
}

func testStream(items ...any) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for _, item := range items {
			if !yield(item, nil) {
				return
			}
		}
	}
}

func assertStreamValues(t *testing.T, got, want []any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("values = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("values[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
