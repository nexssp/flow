package events

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"
)

func BenchmarkKernelBuiltActionDoTypedRequest(b *testing.B) {
	var calls atomic.Uint64
	sink := action.New("bench.direct_typed_sink", func(_ context.Context, req benchmarkTypedSinkRequest) (any, error) {
		if req.Message != "ready" {
			return nil, errors.New("unexpected direct typed sink request")
		}
		calls.Add(1)
		return nil, nil
	}).Build()
	ctx := context.Background()
	request := benchmarkTypedSinkRequest{Message: "ready"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := sink.Do(ctx, request); err != nil {
			b.Fatal(err)
		}
	}
}
