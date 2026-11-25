package events

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/xctx"
)

type benchmarkReadyPayload struct {
	BindAddress string `json:"bind_address"`
}

type benchmarkHostPublisher struct{}

func (benchmarkHostPublisher) PublishEvent(context.Context, string, any) error {
	return nil
}

func newRoutedPublishBenchmark(b *testing.B, withDelegate bool) (context.Context, *benchmarkReadyPayload) {
	b.Helper()
	sink := NewNonRetainingEventSink[benchmarkReadyPayload]("bench.sink", func(_ context.Context, payload *benchmarkReadyPayload) error {
		if payload.BindAddress == "" {
			return errors.New("missing bind_address")
		}
		return nil
	})
	publisher := routedPublisher{routes: &compiledRouteSet{bySubject: map[string]compiledRoute{
		"events.listener_ready": {
			EventName:   "transport.events.listener_ready",
			Subject:     "events.listener_ready",
			BundleID:    "transport",
			PayloadType: reflect.TypeFor[*benchmarkReadyPayload](),
			Sink:        sink,
		},
	}}}
	if withDelegate {
		publisher.delegate = benchmarkHostPublisher{}
	}
	ctx := xctx.WithEventPublisher(context.Background(), publisher)
	payload := &benchmarkReadyPayload{BindAddress: "127.0.0.1:4312"}
	return ctx, payload
}

func BenchmarkRoutedPublishEventTypedSink(b *testing.B) {
	ctx, payload := newRoutedPublishBenchmark(b, false)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := PublishFromBundle(ctx, "transport", "events.listener_ready", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkRoutedPublishEventWithDelegateTypedSink(b *testing.B) {
	ctx, payload := newRoutedPublishBenchmark(b, true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := PublishFromBundle(ctx, "transport", "events.listener_ready", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkRoutedPublishEventParallelTypedSink(b *testing.B) {
	ctx, payload := newRoutedPublishBenchmark(b, true)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if err := PublishFromBundle(ctx, "transport", "events.listener_ready", payload); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkRoutedPublishEventFreshPayload(b *testing.B) {
	ctx, _ := newRoutedPublishBenchmark(b, false)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		payload := &benchmarkReadyPayload{BindAddress: "127.0.0.1:4312"}
		if err := PublishFromBundle(ctx, "transport", "events.listener_ready", payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}
