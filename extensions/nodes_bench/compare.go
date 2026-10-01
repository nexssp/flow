package nodes_bench

import (
	"context"
	"encoding/json"
	"os"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
)

const benchDefaultTolerancePct = 5.0

// BenchCompareReq is a BenchRunRes compared against a baseline file.
// TolerancePct is the allowed regression percentage; a lower reading
// for RPS or a higher reading for latency metrics counts as regression.
type BenchCompareReq struct {
	Baseline     string  `json:"baseline" validate:"required"`
	TolerancePct float64 `json:"tolerance_pct,omitempty"`
	BenchRunRes
}

// Metric is one named comparison.
type Metric struct {
	Baseline  float64 `json:"baseline"`
	Current   float64 `json:"current"`
	DeltaPct  float64 `json:"delta_pct"`
	Regressed bool    `json:"regressed"`
}

// BenchCompareRes aggregates every metric and the overall verdict.
type BenchCompareRes struct {
	Baseline    string            `json:"baseline"`
	Pass        bool              `json:"pass"`
	Regressions []string          `json:"regressions,omitempty"`
	Metrics     map[string]Metric `json:"metrics"`
}

// BenchCompare diffs the current benchmark against a stored baseline.
// A non-zero tolerance allows small fluctuations to pass.
var BenchCompare = action.New("bench.compare", func(_ context.Context, req BenchCompareReq) (BenchCompareRes, error) {
	return runCompare(req)
}).Description("Compare current benchmark against a baseline file").
	Tag("bench", "compare").
	Build()

func runCompare(req BenchCompareReq) (BenchCompareRes, error) {
	clean, err := xfs.Rel(req.Baseline)
	if err != nil {
		return BenchCompareRes{}, err
	}

	tolerance := req.TolerancePct
	if tolerance <= 0 {
		tolerance = benchDefaultTolerancePct
	}

	raw, err := os.ReadFile(clean)
	if err != nil {
		return BenchCompareRes{}, xerr.NotFound("bench.compare: baseline not readable: " + err.Error())
	}

	var baseline BenchRunRes
	if err := json.Unmarshal(raw, &baseline); err != nil {
		return BenchCompareRes{}, xerr.BadRequest("bench.compare: baseline parse: " + err.Error())
	}

	result := BenchCompareRes{
		Baseline: clean,
		Metrics:  make(map[string]Metric, 7),
	}

	// Latency metrics: higher is worse.
	compareWorseIfHigher(result.Metrics, &result.Regressions, "min_ms", baseline.MinMs, req.MinMs, tolerance)
	compareWorseIfHigher(result.Metrics, &result.Regressions, "mean_ms", baseline.MeanMs, req.MeanMs, tolerance)
	compareWorseIfHigher(result.Metrics, &result.Regressions, "p50_ms", baseline.P50Ms, req.P50Ms, tolerance)
	compareWorseIfHigher(result.Metrics, &result.Regressions, "p95_ms", baseline.P95Ms, req.P95Ms, tolerance)
	compareWorseIfHigher(result.Metrics, &result.Regressions, "p99_ms", baseline.P99Ms, req.P99Ms, tolerance)
	compareWorseIfHigher(result.Metrics, &result.Regressions, "max_ms", baseline.MaxMs, req.MaxMs, tolerance)

	// Throughput: higher is better.
	compareWorseIfLower(result.Metrics, &result.Regressions, "rps", baseline.RPS, req.RPS, tolerance)

	result.Pass = len(result.Regressions) == 0
	return result, nil
}

func compareWorseIfHigher(metrics map[string]Metric, regressions *[]string, name string, baseline, current, tolerance float64) {
	if baseline == 0 {
		return
	}
	delta := (current - baseline) / baseline * 100
	regressed := delta > tolerance
	metrics[name] = Metric{Baseline: baseline, Current: current, DeltaPct: delta, Regressed: regressed}
	if regressed {
		*regressions = append(*regressions, name)
	}
}

func compareWorseIfLower(metrics map[string]Metric, regressions *[]string, name string, baseline, current, tolerance float64) {
	if baseline == 0 {
		return
	}
	delta := (current - baseline) / baseline * 100
	regressed := delta < -tolerance
	metrics[name] = Metric{Baseline: baseline, Current: current, DeltaPct: delta, Regressed: regressed}
	if regressed {
		*regressions = append(*regressions, name)
	}
}
