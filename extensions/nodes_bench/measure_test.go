package nodes_bench

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMeasureDurationsOrderingAndErrors(t *testing.T) {
	t.Parallel()

	delays := []time.Duration{8 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}
	calls := 0
	res, err := Measure(context.Background(), len(delays), func(context.Context) error {
		delay := delays[calls]
		calls++
		time.Sleep(delay)
		if calls == 2 {
			return errors.New("callback failed")
		}
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, calls, len(delays))
	ktest.RequireEqual(t, res.Iterations, len(delays))
	ktest.RequireEqual(t, res.Errors, 1)
	ktest.RequireCondition(t, res.MinMs > 0, "minimum duration should be positive")
	ktest.RequireCondition(t, res.MaxMs-res.MinMs >= 3, "expected distinct sorted durations, got min=%v max=%v", res.MinMs, res.MaxMs)
	ktest.RequireCondition(t, res.MinMs <= res.P50Ms && res.P50Ms <= res.P95Ms && res.P95Ms <= res.MaxMs,
		"percentiles are not ordered: min=%v p50=%v p95=%v max=%v", res.MinMs, res.P50Ms, res.P95Ms, res.MaxMs)
	ktest.RequireCondition(t, res.MeanMs >= res.MinMs && res.MeanMs <= res.MaxMs,
		"mean is outside the sample range: min=%v mean=%v max=%v", res.MinMs, res.MeanMs, res.MaxMs)
	ktest.RequireCondition(t, res.RPS > 0, "expected positive RPS")
}

func TestMeasureRejectsInvalidIterations(t *testing.T) {
	t.Parallel()

	for _, iterations := range []int{0, -1, benchMaxIterations + 1} {
		_, err := Measure(context.Background(), iterations, func(context.Context) error { return nil })
		ktest.RequireErrorKind(t, err, xerr.KindBadRequest)
	}
}

func TestMeasureCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := Measure(ctx, 3, func(context.Context) error {
		calls++
		cancel()
		return nil
	})
	ktest.RequireCondition(t, errors.Is(err, context.Canceled), "expected context cancellation, got %v", err)
	ktest.RequireEqual(t, calls, 1)
}

func TestMeasureRecoversCallbackPanic(t *testing.T) {
	t.Parallel()

	res, err := Measure(context.Background(), 1, func(context.Context) error {
		panic("callback panic")
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, res.Errors, 1)
}
