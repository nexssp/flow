package events_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/events"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

const compiledRouteBenchmarkSource = `@events {
  observe eventperf.events.ready -> eventperf.sink
}
`

const compiledPipelineRouteBenchmarkSource = `@schema EventReady {
  bind_address! string
}
@pipeline consume_ready(payload: !EventReady)
  eventperf.discard(bind_address: payload.bind_address)
@end
@events {
  observe eventperf.events.ready -> pipeline.consume_ready
}
`

const compiledRouteBenchmarkSinkMessage = "127.0.0.1:4312"

var errUnexpectedCompiledRouteBenchmarkMessage = errors.New("unexpected compiled route benchmark payload")

type compiledRouteBenchmarkPayload struct {
	BindAddress string `json:"bind_address"`
}

type compiledRouteBenchmarkHost struct {
	calls atomic.Uint64
}

func (host *compiledRouteBenchmarkHost) PublishEvent(context.Context, string, any) error {
	host.calls.Add(1)
	return nil
}

func newCompiledRoutePublishBenchmark(b *testing.B, withDelegate bool) (context.Context, *compiledRouteBenchmarkPayload) {
	b.Helper()

	var sinkCalls atomic.Uint64
	sink := events.NewNonRetainingEventSink[compiledRouteBenchmarkPayload]("eventperf.sink", func(_ context.Context, payload *compiledRouteBenchmarkPayload) error {
		if payload.BindAddress != compiledRouteBenchmarkSinkMessage {
			return errUnexpectedCompiledRouteBenchmarkMessage
		}
		sinkCalls.Add(1)
		return nil
	})
	bundle := compiler.Bundle{
		ID:    "eventperf",
		Alias: "eventperf",
		Libraries: []action.Library{{
			Name:    "eventperf",
			Actions: []action.AnyAction{sink},
		}},
		Events: []compiler.EventSpec{{
			Name:        "eventperf.events.ready",
			Subject:     "events.perf_ready",
			Description: "benchmark listener-ready event",
			PayloadType: reflect.TypeFor[compiledRouteBenchmarkPayload](),
		}},
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		b.Fatal(err)
	}
	compiled, err := runner.Compile(context.Background(), cfg, compiledRouteBenchmarkSource, "compiled-route-benchmark.nflow")
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	if withDelegate {
		ctx = xctx.WithEventPublisher(ctx, &compiledRouteBenchmarkHost{})
	}
	ctx = events.InstallPublisher(ctx, compiled.Meta)
	publisher, ok := xctx.EventPublisherFrom(ctx)
	if !ok || publisher == nil {
		b.Fatal("compiled event route did not install its publisher")
	}
	payload := &compiledRouteBenchmarkPayload{BindAddress: compiledRouteBenchmarkSinkMessage}
	return ctx, payload
}

func BenchmarkCompiledRoutedPublishEvent(b *testing.B) {
	ctx, payload := newCompiledRoutePublishBenchmark(b, false)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := events.PublishFromBundle(ctx, "eventperf", "events.perf_ready", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkCompiledRoutedPublishEventPipeline(b *testing.B) {
	discard := action.New("eventperf.discard", func(context.Context, map[string]any) (any, error) {
		return nil, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:    "eventperf",
		Alias: "eventperf",
		Libraries: []action.Library{{
			Name:    "eventperf",
			Actions: []action.AnyAction{discard},
		}},
		Events: []compiler.EventSpec{{
			Name:        "eventperf.events.ready",
			Subject:     "events.perf_ready",
			Description: "benchmark listener-ready event",
			PayloadType: reflect.TypeFor[compiledRouteBenchmarkPayload](),
		}},
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		b.Fatal(err)
	}
	compiled, err := runner.Compile(context.Background(), cfg, compiledPipelineRouteBenchmarkSource, "compiled-pipeline-route-benchmark.nflow")
	if err != nil {
		b.Fatal(err)
	}
	ctx := events.InstallPublisher(context.Background(), compiled.Meta)
	payload := &compiledRouteBenchmarkPayload{BindAddress: compiledRouteBenchmarkSinkMessage}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := events.PublishFromBundle(ctx, "eventperf", "events.perf_ready", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkCompiledRoutedPublishEventWithFreshPayload(b *testing.B) {
	ctx, _ := newCompiledRoutePublishBenchmark(b, false)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		payload := &compiledRouteBenchmarkPayload{BindAddress: compiledRouteBenchmarkSinkMessage}
		if err := events.PublishFromBundle(ctx, "eventperf", "events.perf_ready", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkCompiledRoutedPublishEventWithDelegate(b *testing.B) {
	ctx, payload := newCompiledRoutePublishBenchmark(b, true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := events.PublishFromBundle(ctx, "eventperf", "events.perf_ready", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkCompiledRoutedPublishEventParallel(b *testing.B) {
	ctx, payload := newCompiledRoutePublishBenchmark(b, false)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if err := events.PublishFromBundle(ctx, "eventperf", "events.perf_ready", payload); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}
