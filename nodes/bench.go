// path: nexssp/flow/nodes/bench.go
//
// Benchmark nodes.
//
// Design notes:
//   - bench.run captures the registry at construction. The registry is a
//     live pointer, so actions registered after construction are visible
//     at invoke time.
//   - Per-iteration work is a single time.Now / time.Since pair and a
//     slice write. No maps, no reflection, no fmt on the measured path.
//   - Panic in a target counts as one error and does not abort the run.
//     The defer is function-scoped so the compiler can open-code it.
//   - bench.save and bench.compare embed BenchRunRes so they chain
//     directly from bench.run without a projection step in the flow.
//   - All caller-supplied paths go through kernel/xfs.Rel: absolute
//     paths, ".." traversal, ":" (Windows ADS/drive), NUL and control
//     characters, trailing dots, and reserved device names are rejected
//     before any filesystem call.
package nodes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
)

// ── bench.run ──────────────────────────────────────────────────────────────

type BenchRunReq struct {
	Action     string         `json:"action"               validate:"required" usage:"Node name to benchmark"`
	Iterations int            `json:"iterations,omitempty"                    usage:"Timed iterations (default 50)"`
	Warmup     int            `json:"warmup,omitempty"                        usage:"Discarded pre-runs (default 3)"`
	Payload    map[string]any `json:"payload,omitempty"                       usage:"Request passed to each invocation"`
}

// BenchRunRes is the measured distribution. All durations are milliseconds.
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

const (
	benchDefaultIterations = 50
	benchDefaultWarmup     = 3
	benchMaxIterations     = 1_000_000
)

func NewBenchRunAction(reg contracts.Registry) action.AnyAction {
	return action.New("bench.run", func(ctx context.Context, req BenchRunReq) (BenchRunRes, error) {
		return runBenchmark(ctx, reg, req)
	}).
		Description("Run an action N times and report its latency distribution").
		Tag("bench", "perf").
		Build()
}

func runBenchmark(ctx context.Context, reg contracts.Registry, req BenchRunReq) (BenchRunRes, error) {
	if reg == nil {
		return BenchRunRes{}, xerr.Internal("bench.run: registry is nil")
	}

	if req.Iterations <= 0 {
		req.Iterations = benchDefaultIterations
	}

	if req.Iterations > benchMaxIterations {
		return BenchRunRes{}, xerr.BadRequest("bench.run: iterations exceeds limit")
	}

	if req.Warmup < 0 {
		req.Warmup = benchDefaultWarmup
	}

	target, ok := reg.Get(req.Action)
	if !ok {
		return BenchRunRes{}, xerr.NotFound("bench.run: action not found: " + req.Action)
	}

	for i := 0; i < req.Warmup; i++ {
		if err := ctx.Err(); err != nil {
			return BenchRunRes{}, err
		}

		_ = invokeOnce(ctx, target, req.Payload)
	}

	samples := make([]time.Duration, req.Iterations)

	var errors, sum int64

	start := time.Now()

	for i := 0; i < req.Iterations; i++ {
		if err := ctx.Err(); err != nil {
			return BenchRunRes{}, err
		}

		t0 := time.Now()
		err := invokeOnce(ctx, target, req.Payload)
		d := time.Since(t0)
		samples[i] = d
		sum += int64(d)

		if err != nil {
			errors++
		}
	}

	elapsed := time.Since(start)

	slices.Sort(samples)

	return BenchRunRes{
		Action:     req.Action,
		Iterations: req.Iterations,
		Warmup:     req.Warmup,
		Errors:     int(errors),
		MinMs:      toMs(samples[0]),
		MaxMs:      toMs(samples[len(samples)-1]),
		MeanMs:     toMs(time.Duration(sum / int64(req.Iterations))),
		P50Ms:      toMs(percentile(samples, 0.50)),
		P95Ms:      toMs(percentile(samples, 0.95)),
		P99Ms:      toMs(percentile(samples, 0.99)),
		RPS:        float64(req.Iterations) / elapsed.Seconds(),
		ElapsedMs:  elapsed.Milliseconds(),
	}, nil
}

// invokeOnce executes one iteration with panic isolation. Function-scoped
// defer keeps this out of the hot loop's escape analysis.
func invokeOnce(ctx context.Context, act action.AnyAction, payload any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = xerr.Internal("bench.run: iteration panicked")
		}
	}()

	_, err = action.InvokeAny(ctx, act, payload)

	return err
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	idx := int(float64(len(sorted))*p) - 1
	if idx < 0 {
		idx = 0
	}

	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

func toMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// ── bench.save ─────────────────────────────────────────────────────────────

type BenchSaveReq struct {
	File string `json:"file" validate:"required" usage:"Relative path to write (validated by xfs.Rel)"`
	BenchRunRes
}

type BenchSaveRes struct {
	File  string `json:"file"`
	Bytes int    `json:"bytes"`
	BenchRunRes
}

func NewBenchSaveAction() action.AnyAction {
	return action.New("bench.save", func(_ context.Context, req BenchSaveReq) (BenchSaveRes, error) {
		clean, err := xfs.Rel(req.File)
		if err != nil {
			return BenchSaveRes{}, err
		}

		data, err := json.MarshalIndent(req.BenchRunRes, "", "  ")
		if err != nil {
			return BenchSaveRes{}, xerr.Internal("bench.save: marshal", err)
		}

		if dir := filepath.Dir(clean); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return BenchSaveRes{}, xerr.Internal("bench.save: mkdir", err)
			}
		}

		if err := os.WriteFile(clean, data, 0o600); err != nil {
			return BenchSaveRes{}, xerr.Internal("bench.save: write", err)
		}

		return BenchSaveRes{
			File:        clean,
			Bytes:       len(data),
			BenchRunRes: req.BenchRunRes,
		}, nil
	}).
		Description("Write benchmark results to a JSON file").
		Tag("bench", "io").
		Build()
}

// ── bench.compare ──────────────────────────────────────────────────────────

type BenchCompareReq struct {
	Baseline     string  `json:"baseline" validate:"required" usage:"Baseline JSON path (validated by xfs.Rel)"`
	TolerancePct float64 `json:"tolerance_pct,omitempty"      usage:"Allowed regression percent (default 5)"`
	BenchRunRes
}

type BenchMetric struct {
	Baseline  float64 `json:"baseline"`
	Current   float64 `json:"current"`
	DeltaPct  float64 `json:"delta_pct"`
	Regressed bool    `json:"regressed"`
}

type BenchCompareRes struct {
	Baseline    string                 `json:"baseline"`
	Pass        bool                   `json:"pass"`
	Regressions []string               `json:"regressions,omitempty"`
	Metrics     map[string]BenchMetric `json:"metrics"`
}

const benchDefaultTolerancePct = 5.0

func NewBenchCompareAction() action.AnyAction {
	return action.New("bench.compare", func(_ context.Context, req BenchCompareReq) (BenchCompareRes, error) {
		baseline, err := xfs.Rel(req.Baseline)
		if err != nil {
			return BenchCompareRes{}, err
		}

		tol := req.TolerancePct
		if tol <= 0 {
			tol = benchDefaultTolerancePct
		}

		raw, err := os.ReadFile(baseline)
		if err != nil {
			return BenchCompareRes{}, xerr.NotFound("bench.compare: baseline not readable: " + err.Error())
		}

		var base BenchRunRes
		if err := json.Unmarshal(raw, &base); err != nil {
			return BenchCompareRes{}, xerr.BadRequest("bench.compare: baseline parse: " + err.Error())
		}

		res := BenchCompareRes{
			Baseline: baseline,
			Metrics:  make(map[string]BenchMetric, 8),
		}

		compareLower(res.Metrics, &res.Regressions, "min_ms", base.MinMs, req.MinMs, tol)
		compareLower(res.Metrics, &res.Regressions, "mean_ms", base.MeanMs, req.MeanMs, tol)
		compareLower(res.Metrics, &res.Regressions, "p50_ms", base.P50Ms, req.P50Ms, tol)
		compareLower(res.Metrics, &res.Regressions, "p95_ms", base.P95Ms, req.P95Ms, tol)
		compareLower(res.Metrics, &res.Regressions, "p99_ms", base.P99Ms, req.P99Ms, tol)
		compareLower(res.Metrics, &res.Regressions, "max_ms", base.MaxMs, req.MaxMs, tol)
		compareHigher(res.Metrics, &res.Regressions, "rps", base.RPS, req.RPS, tol)

		res.Pass = len(res.Regressions) == 0

		return res, nil
	}).
		Description("Compare current benchmark against a baseline file").
		Tag("bench", "compare").
		Build()
}

func compareLower(dst map[string]BenchMetric, regs *[]string, name string, base, cur, tol float64) {
	if base == 0 {
		return
	}

	delta := (cur - base) / base * 100
	regressed := delta > tol

	dst[name] = BenchMetric{Baseline: base, Current: cur, DeltaPct: delta, Regressed: regressed}
	if regressed {
		*regs = append(*regs, name)
	}
}

func compareHigher(dst map[string]BenchMetric, regs *[]string, name string, base, cur, tol float64) {
	if base == 0 {
		return
	}

	delta := (cur - base) / base * 100
	regressed := delta < -tol

	dst[name] = BenchMetric{Baseline: base, Current: cur, DeltaPct: delta, Regressed: regressed}
	if regressed {
		*regs = append(*regs, name)
	}
}
