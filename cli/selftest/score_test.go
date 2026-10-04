package selftest

import (
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// ── Correctness ──────────────────────────────────────────────────────

func TestScore_SkippedDoesNotReduceValue(t *testing.T) {
	// 91 passed, 0 failed, 1 skipped. The skipped feature was
	// intentionally not run in this environment (the wasm fixture).
	// Counting it would make the same code report a different grade
	// on a different host.
	s := Score{Passed: 91, Failed: 0, Skipped: 1, Total: 92}

	ktest.RequireEqual(t, s.Scored(), 91)
	ktest.RequireEqual(t, s.Value(), 100.0)
	ktest.RequireEqual(t, s.Grade(), "A+")
}

func TestScore_FailureReducesValue(t *testing.T) {
	s := Score{Passed: 91, Failed: 1, Skipped: 0, Total: 92}

	ktest.RequireEqual(t, s.Scored(), 92)
	ktest.RequireCondition(t, s.Value() < 100,
		"expected < 100, got %v", s.Value())
	ktest.RequireEqual(t, s.Grade(), "A")
}

func TestScore_AllSkipped_ValueIsZero(t *testing.T) {
	s := Score{Skipped: 5, Total: 5}
	ktest.RequireEqual(t, s.Scored(), 0)
	ktest.RequireEqual(t, s.Value(), 0.0)
}

func TestScore_GradeThresholds(t *testing.T) {
	cases := []struct {
		passed int
		failed int
		want   string
	}{
		{100, 0, "A+"},
		{99, 1, "A"},  // 99.0
		{85, 15, "B"}, // 85.0
		{70, 30, "C"}, // 70.0
		{50, 50, "D"}, // 50.0
		{49, 51, "F"}, // 49.0
	}
	for _, tc := range cases {
		s := Score{Passed: tc.passed, Failed: tc.failed}
		ktest.RequireEqual(t, s.Grade(), tc.want)
	}
}

// ── Backward compatibility: flat fields stay where they were ─────────

func TestScore_FlatFieldsStillReadable(t *testing.T) {
	// Every caller that reads s.TotalAllocs, s.Slowest, or
	// s.Fingerprint directly must keep compiling. This test is the
	// contract: if a future change nests these into a sub-struct,
	// this test fails to build.
	s := Score{
		TotalAllocs:        12345,
		TotalAllocBytes:    67890,
		TotalCompileAllocs: 111,
		TotalRunAllocs:     222,
		Fingerprint:        "abc",
		Slowest:            []FeatureStat{{Name: "x"}},
		TopAllocators:      []FeatureStat{{Name: "y"}},
	}

	ktest.RequireEqual(t, s.TotalAllocs, uint64(12345))
	ktest.RequireEqual(t, s.TotalAllocBytes, uint64(67890))
	ktest.RequireEqual(t, s.TotalCompileAllocs, uint64(111))
	ktest.RequireEqual(t, s.TotalRunAllocs, uint64(222))
	ktest.RequireEqual(t, s.Fingerprint, "abc")
	ktest.RequireLen(t, s.Slowest, 1)
	ktest.RequireLen(t, s.TopAllocators, 1)
}

// ── PerfBaseline thresholds ──────────────────────────────────────────

func TestPerfBaseline_WithinTolerance_NotRegressed(t *testing.T) {
	// A run that grew allocations by 3% is within the 5% tolerance
	// and must not be flagged. Without the threshold, every run
	// against a baseline recorded with a single extra allocation
	// would report a regression.
	pb := PerfBaseline{
		AllocsDeltaPct:  3.0,
		BytesDeltaPct:   1.0,
		CompileDeltaPct: 2.0,
		RunDeltaPct:     4.0,
	}
	pb.Regressed = pb.AllocsDeltaPct > BaselineTolerancePct ||
		pb.BytesDeltaPct > BaselineTolerancePct ||
		pb.CompileDeltaPct > BaselineTolerancePct ||
		pb.RunDeltaPct > BaselineTolerancePct

	ktest.RequireFalse(t, pb.Regressed)
}

func TestPerfBaseline_OverTolerance_Regressed(t *testing.T) {
	pb := PerfBaseline{
		AllocsDeltaPct:  12.0,
		BytesDeltaPct:   8.0,
		CompileDeltaPct: 2.0,
		RunDeltaPct:     3.0,
	}
	pb.Regressed = pb.AllocsDeltaPct > BaselineTolerancePct ||
		pb.BytesDeltaPct > BaselineTolerancePct ||
		pb.CompileDeltaPct > BaselineTolerancePct ||
		pb.RunDeltaPct > BaselineTolerancePct

	ktest.RequireTrue(t, pb.Regressed)
}

func TestPctDelta_ZeroBaseline(t *testing.T) {
	// A zero baseline has no meaningful percentage change. Returning
	// 0 rather than +Inf keeps the delta line printable.
	ktest.RequireEqual(t, pctDelta(0, 100), 0.0)
}

func TestPctDelta_Growth(t *testing.T) {
	ktest.RequireEqual(t, pctDelta(100, 110), 10.0)
	ktest.RequireEqual(t, pctDelta(100, 90), -10.0)
}

// ── ComputeScore wiring ──────────────────────────────────────────────

func TestComputeScore_FillsFlatFields(t *testing.T) {
	results := []Result{
		{SelfTestFeature: feature("a"), Status: StatusPass, Elapsed: 10 * time.Millisecond, CompileAllocs: 5, RunAllocs: 3},
		{SelfTestFeature: feature("b"), Status: StatusPass, Elapsed: 20 * time.Millisecond, CompileAllocs: 7, RunAllocs: 4},
		{SelfTestFeature: feature("c"), Status: StatusSkip, SkipReason: "host-specific"},
	}

	s := ComputeScore(results)

	ktest.RequireEqual(t, s.Passed, 2)
	ktest.RequireEqual(t, s.Failed, 0)
	ktest.RequireEqual(t, s.Skipped, 1)
	ktest.RequireEqual(t, s.Total, 3)
	ktest.RequireEqual(t, s.Value(), 100.0)

	ktest.RequireEqual(t, s.TotalAllocs, uint64(19)) // 5+7+3+4
	ktest.RequireEqual(t, s.TotalCompileAllocs, uint64(12))
	ktest.RequireEqual(t, s.TotalRunAllocs, uint64(7))

	ktest.RequireLen(t, s.Slowest, 2) // skip excluded
	ktest.RequireLen(t, s.TopAllocators, 2)
	ktest.RequireNotNil(t, s.Fingerprint)
}

func feature(name string) core.SelfTestFeature {
	return core.SelfTestFeature{Name: name, DSL: "noop"}
}
