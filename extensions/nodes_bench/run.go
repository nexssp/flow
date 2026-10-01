package nodes_bench

import (
	"context"
	"slices"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
)

const (
	benchDefaultIterations = 50
	benchDefaultWarmup     = 3
	benchMaxIterations     = 1_000_000
)

type BenchRunReq struct {
	Action     string         `json:"action"     validate:"required"`
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

	samples := make([]time.Duration, req.Iterations)
	var errorsCount int
	var sum time.Duration
	started := time.Now()

	for i := range req.Iterations {
		if err := ctx.Err(); err != nil {
			return BenchRunRes{}, err
		}
		sampleStart := time.Now()
		err := invokeOnce(ctx, target, req.Payload)
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
		Action:     req.Action,
		Iterations: req.Iterations,
		Warmup:     req.Warmup,
		Errors:     errorsCount,
		MinMs:      toMillis(samples[0]),
		MaxMs:      toMillis(samples[len(samples)-1]),
		MeanMs:     toMillis(sum / time.Duration(req.Iterations)),
		P50Ms:      toMillis(percentile(samples, 0.50)),
		P95Ms:      toMillis(percentile(samples, 0.95)),
		P99Ms:      toMillis(percentile(samples, 0.99)),
		RPS:        float64(req.Iterations) * 1e9 / float64(nanos),
		ElapsedMs:  totalElapsed.Milliseconds(),
	}, nil
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
