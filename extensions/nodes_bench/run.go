package nodes_bench

import (
	"context"
	"slices"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/contracts"
)

const (
	benchDefaultIterations = 50
	benchDefaultWarmup     = 3
	benchMaxIterations     = 1_000_000
)

type BenchRunReq struct {
	Action     string         `json:"action"               validate:"required"`
	Iterations int            `json:"iterations,omitempty"`
	Warmup     int            `json:"warmup,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type BenchRunRes struct {
	Action     string  `json:"action"`
	Iterations int     `json:"iterations"`
	Warmup     int     `json:"warmup"`
	Errors     int     `json:"errors"`
	MinMs      float64 `json:"min_ms"`
	MaxMs      float64 `json:"max_ms"`
	MeanMs     float64 `json:"mean_ms"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	P99Ms      float64 `json:"p99_ms"`
	RPS        float64 `json:"rps"`
	ElapsedMs  int64   `json:"elapsed_ms"`
}

var BenchRun = action.New("bench.run", func(ctx context.Context, req BenchRunReq) (BenchRunRes, error) {
	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return BenchRunRes{}, xerr.Internal("bench.run: no action resolver in context")
	}
	return runBenchmark(ctx, resolver, req)
}).Description("Run an action N times and report latency distribution").
	Tag("bench", "perf").
	Build()

func runBenchmark(ctx context.Context, resolver contracts.ActionResolver, req BenchRunReq) (BenchRunRes, error) {
	if req.Iterations <= 0 {
		req.Iterations = benchDefaultIterations
	}
	if req.Iterations > benchMaxIterations {
		return BenchRunRes{}, xerr.BadRequest("bench.run: iterations exceeds limit")
	}
	if req.Warmup < 0 {
		req.Warmup = benchDefaultWarmup
	}

	target, ok := resolver.Action(req.Action)
	if !ok {
		return BenchRunRes{}, xerr.NotFound("bench.run: action not found: " + req.Action)
	}

	for range req.Warmup {
		if err := ctx.Err(); err != nil {
			return BenchRunRes{}, err
		}
		invokeOnce(ctx, target, req.Payload) //nolint:errcheck // warmup errors are not counted
	}

	res, err := Measure(ctx, req.Iterations, func(ctx context.Context) error {
		_, err := action.InvokeAny(ctx, target, req.Payload)
		return err
	})
	if err != nil {
		return BenchRunRes{}, err
	}
	res.Action = req.Action
	res.Warmup = req.Warmup
	return res, nil
}

func invokeOnce(ctx context.Context, target action.AnyAction, payload any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = xerr.PanicRecovery(recovered)
		}
	}()
	_, err = action.InvokeAny(ctx, target, payload)
	return err
}

// Measure invokes callback iterations times and reports the latency distribution.
// It does not perform warmup; callers that need warmup should do so before calling Measure.
func Measure(ctx context.Context, iterations int, invoke func(context.Context) error) (BenchRunRes, error) {
	if iterations < 1 || iterations > benchMaxIterations {
		return BenchRunRes{}, xerr.BadRequest("bench.measure: iterations must be between 1 and 1000000")
	}

	samples := make([]time.Duration, iterations)
	var errorsCount int
	var sum time.Duration
	started := time.Now()

	for i := range iterations {
		if err := ctx.Err(); err != nil {
			return BenchRunRes{}, err
		}
		sampleStart := time.Now()
		err := invokeSafely(ctx, invoke)
		elapsed := time.Since(sampleStart)
		samples[i] = elapsed
		sum += elapsed
		if err != nil {
			errorsCount++
		}
	}

	totalElapsed := time.Since(started)
	slices.Sort(samples)
	nanos := max(totalElapsed.Nanoseconds(), 1)

	return BenchRunRes{
		Iterations: iterations,
		Errors:     errorsCount,
		MinMs:      toMillis(samples[0]),
		MaxMs:      toMillis(samples[len(samples)-1]),
		MeanMs:     toMillis(sum / time.Duration(iterations)),
		P50Ms:      toMillis(percentile(samples, 0.50)),
		P95Ms:      toMillis(percentile(samples, 0.95)),
		P99Ms:      toMillis(percentile(samples, 0.99)),
		RPS:        float64(iterations) * 1e9 / float64(nanos),
		ElapsedMs:  totalElapsed.Milliseconds(),
	}, nil
}

func invokeSafely(ctx context.Context, invoke func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = xerr.PanicRecovery(recovered)
		}
	}()
	return invoke(ctx)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := max(int(float64(len(sorted))*p)-1, 0)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func toMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
