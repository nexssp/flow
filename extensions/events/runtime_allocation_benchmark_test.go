package events

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

func BenchmarkEventPayloadProjection(b *testing.B) {
	projector, err := compiler.NewExpressionProjector(reflect.TypeFor[benchmarkReadyPayload]())
	if err != nil {
		b.Fatal(err)
	}
	var payload any = benchmarkReadyPayload{BindAddress: "127.0.0.1:4312"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := payloadExpressionBinding(payload, projector); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEventRouteScopeSetup(b *testing.B) {
	baseCtx := context.Background()
	binding := map[string]any{"bind_address": "127.0.0.1:4312"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx := WithRouteInvocation(baseCtx)
		ctx = compiler.WithExpressionScope(ctx, binding)
		if err := compiler.BindPipelineInputValue(ctx, "ready", binding); err != nil {
			b.Fatal(err)
		}
		runtime.KeepAlive(ctx)
	}
}

func BenchmarkExpressionEnvironmentMerge(b *testing.B) {
	ctx := compiler.WithExpressionScope(context.Background(), map[string]any{"root": "input"})
	if err := compiler.BindPipelineInputValue(ctx, "ready", map[string]any{"bind_address": "127.0.0.1:4312"}); err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"message": "constant"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		merged := compiler.MergeSymbolNamespaces(ctx, env)
		runtime.KeepAlive(merged)
	}
}

type benchmarkTypedSinkRequest struct {
	Message string `json:"message"`
}

var errUnexpectedBenchmarkTypedSinkRequest = errors.New("unexpected benchmark typed sink request")

func newBenchmarkTypedSink(calls *atomic.Uint64) action.AnyAction {
	return action.New("bench.typed_sink", func(_ context.Context, req benchmarkTypedSinkRequest) (any, error) {
		if req.Message != "ready" {
			return nil, errUnexpectedBenchmarkTypedSinkRequest
		}
		calls.Add(1)
		return nil, nil
	}).Build()
}

func BenchmarkKernelInvokeAnyTypedRequest(b *testing.B) {
	var calls atomic.Uint64
	sink := newBenchmarkTypedSink(&calls)
	ctx := context.Background()
	request := benchmarkTypedSinkRequest{Message: "ready"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := action.InvokeAny(ctx, sink, request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkKernelInvokeAnyMapRequest(b *testing.B) {
	var calls atomic.Uint64
	sink := newBenchmarkTypedSink(&calls)
	ctx := context.Background()
	request := map[string]any{"message": "ready"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := action.InvokeAny(ctx, sink, request); err != nil {
			b.Fatal(err)
		}
	}
}
