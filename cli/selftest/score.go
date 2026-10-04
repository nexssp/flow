package selftest

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

// FeatureStat is one row in the slowest/top-allocator rankings.
type FeatureStat struct {
	Name          string        `json:"name"`
	Section       string        `json:"section,omitempty"`
	Elapsed       time.Duration `json:"elapsed_ns"`
	Allocs        uint64        `json:"allocs"`
	CompileAllocs uint64        `json:"compile_allocs"`
	RunAllocs     uint64        `json:"run_allocs"`
}

// PerfBaseline is the allocation comparison against a previously
// saved baseline. It is populated by ComputeScore only when a
// baseline file exists and was recorded on the same OS/arch as the
// current run.
//
// Timing is deliberately absent. It is host-specific and would
// produce a false regression on any machine that is not the one the
// baseline was recorded on. Allocations are deterministic for the
// same code and toolchain, so they are what the delta tracks.
//
// Regressed is a threshold, not any increase: a run that grew
// allocations by 3% when the baseline was recorded with one fewer
// allocation is not a regression, and a boolean that flipped on any
// increase would be noise instead of signal.
type PerfBaseline struct {
	Allocs        uint64 `json:"allocs"`
	AllocBytes    uint64 `json:"alloc_bytes"`
	CompileAllocs uint64 `json:"compile_allocs"`
	RunAllocs     uint64 `json:"run_allocs"`

	AllocsDeltaPct  float64 `json:"allocs_delta_pct"`
	BytesDeltaPct   float64 `json:"bytes_delta_pct"`
	CompileDeltaPct float64 `json:"compile_delta_pct"`
	RunDeltaPct     float64 `json:"run_delta_pct"`

	Regressed bool `json:"regressed"`
}

// BaselineTolerancePct is the allocation growth, in percent, that does
// not count as a regression.
const BaselineTolerancePct = 5.0

// Score summarizes one self-test run.
//
// Two concerns are reported from this type, deliberately distinguished
// by the fields they read:
//
//   - Correctness — Value() and Grade(). A single number because it
//     answers a single question: did the code do what it should. The
//     denominator excludes skipped features; a skipped feature was
//     intentionally not exercised and cannot say anything about
//     whether the code is correct.
//
//   - Performance — the raw allocation and timing measurements
//     (TotalAllocs, Slowest, TopAllocators), plus the delta against a
//     stored baseline (PerfBaseline). Several numbers because they
//     answer several questions — did allocations grow, did they grow
//     in the compile phase or the run phase, did any metric cross the
//     tolerance.
//
// There is no single "benchmark score". A number that merges
// allocations, bytes, compile, and run into one figure hides which
// metric moved; the deltas are what a reader acts on.
type Score struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Total   int `json:"total"`

	Fingerprint string `json:"fingerprint"`

	TotalAllocs        uint64 `json:"total_allocs"`
	TotalAllocBytes    uint64 `json:"total_alloc_bytes"`
	TotalCompileAllocs uint64 `json:"total_compile_allocs"`
	TotalRunAllocs     uint64 `json:"total_run_allocs"`

	Slowest       []FeatureStat `json:"slowest,omitempty"`
	TopAllocators []FeatureStat `json:"top_allocators,omitempty"`

	// PerfBaseline is present only when a baseline file exists and
	// was recorded on the current OS/arch. It is nil on a first run.
	PerfBaseline *PerfBaseline `json:"perf_baseline,omitempty"`

	BaselineFingerprint string `json:"baseline_fingerprint,omitempty"`
	BaselineMatched     bool   `json:"baseline_matched,omitempty"`
	BaselinePresent     bool   `json:"baseline_present,omitempty"`
}

// Scored returns the denominator for the correctness ratio: features
// that were actually exercised.
func (s Score) Scored() int { return s.Passed + s.Failed }

// Value returns the correctness score in [0, 100].
//
// The denominator is Scored(), not Total. A skipped feature was
// intentionally not run in this environment; counting it would make
// the same code report a different grade on a different host.
func (s Score) Value() float64 {
	if s.Scored() == 0 {
		return 0
	}
	return float64(s.Passed) / float64(s.Scored()) * 100
}

// Grade maps the score to a letter. The thresholds are chosen so that
// a single failure in a 92-feature suite lands at A (98.9), which is
// what a reviewer expects to see: visible, not hidden behind an A+.
func (s Score) Grade() string {
	switch v := s.Value(); {
	case v >= 100:
		return "A+"
	case v >= 95:
		return "A"
	case v >= 85:
		return "B"
	case v >= 70:
		return "C"
	case v >= 50:
		return "D"
	default:
		return "F"
	}
}

const topN = 5

// ComputeScore builds a Score from a completed run's results and
// attaches baseline comparisons when a baseline file exists.
func ComputeScore(results []Result) Score {
	s := Score{Total: len(results)}
	for i := range results {
		r := &results[i]
		switch r.Status {
		case StatusPass:
			s.Passed++
		case StatusFail:
			s.Failed++
		case StatusSkip:
			s.Skipped++
		}
		if r.Status != StatusSkip {
			s.TotalAllocs += r.TotalAllocs()
			s.TotalAllocBytes += r.TotalAllocBytes()
			s.TotalCompileAllocs += r.CompileAllocs
			s.TotalRunAllocs += r.RunAllocs
		}
	}
	s.Fingerprint = fingerprintOf(results)
	s.Slowest = topSlowest(results, topN)
	s.TopAllocators = topAllocators(results, topN)
	s.PerfBaseline = perfDeltaAgainstBaseline(s)

	if b, ok := loadBaseline(); ok {
		s.BaselinePresent = true
		s.BaselineFingerprint = b.Fingerprint
		s.BaselineMatched = b.Fingerprint == s.Fingerprint
	}
	return s
}

// perfDeltaAgainstBaseline compares the current allocations against
// the recorded baseline when one exists and was recorded on the same
// OS/arch as the current run.
//
// A cross-host comparison is silently omitted: the numbers would
// differ for reasons the reader cannot act on, and a false regression
// flag is worse than no flag. The baseline file records saved_host
// for exactly this decision.
func perfDeltaAgainstBaseline(s Score) *PerfBaseline {
	b, ok := loadBaseline()
	if !ok || !b.hasPerformance() {
		return nil
	}
	if b.SavedHost != "" && b.SavedHost != currentHostSignature() {
		return nil
	}

	pb := &PerfBaseline{
		Allocs:        b.Allocs,
		AllocBytes:    b.AllocBytes,
		CompileAllocs: b.CompileAllocs,
		RunAllocs:     b.RunAllocs,
	}
	pb.AllocsDeltaPct = pctDelta(b.Allocs, s.TotalAllocs)
	pb.BytesDeltaPct = pctDelta(b.AllocBytes, s.TotalAllocBytes)
	pb.CompileDeltaPct = pctDelta(b.CompileAllocs, s.TotalCompileAllocs)
	pb.RunDeltaPct = pctDelta(b.RunAllocs, s.TotalRunAllocs)

	pb.Regressed = pb.AllocsDeltaPct > BaselineTolerancePct ||
		pb.BytesDeltaPct > BaselineTolerancePct ||
		pb.CompileDeltaPct > BaselineTolerancePct ||
		pb.RunDeltaPct > BaselineTolerancePct

	return pb
}

// pctDelta returns the percentage change from baseline to current. A
// zero baseline has no meaningful percentage change, so it returns 0
// rather than +Inf.
func pctDelta(baseline, current uint64) float64 {
	if baseline == 0 {
		return 0
	}
	return (float64(current) - float64(baseline)) / float64(baseline) * 100
}

// ── persistence ──────────────────────────────────────────────────────

const baselinePath = ".nflow_selftest_baseline.json"

// baselineFile is the on-disk shape. It carries the correctness
// fingerprint, the correctness counts, the performance measurements,
// the tolerance in effect when it was written, and the host signature
// that produced it.
//
// saved_at and saved_host exist so a CI report can warn when a
// baseline is being compared against a run on a different platform.
// Allocation counts differ substantially across OS/arch for the same
// code; a cross-platform comparison would look like a regression when
// it is not.
type baselineFile struct {
	Fingerprint string `json:"fingerprint"`

	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Total   int `json:"total"`

	Allocs        uint64 `json:"allocs"`
	AllocBytes    uint64 `json:"alloc_bytes"`
	CompileAllocs uint64 `json:"compile_allocs"`
	RunAllocs     uint64 `json:"run_allocs"`

	TolerancePct float64   `json:"tolerance_pct"`
	SavedAt      time.Time `json:"saved_at"`
	SavedHost    string    `json:"saved_host"`
}

// hasPerformance reports whether the baseline carries performance
// measurements. A baseline written by an earlier tool version carries
// only the fingerprint; a delta against it would be a delta against
// zero.
func (b baselineFile) hasPerformance() bool {
	return b.Allocs > 0 || b.AllocBytes > 0
}

// SaveBaseline writes the current run's fingerprint, counts, and
// measurements to the working directory. Overwrites any existing
// baseline; the caller is expected to invoke this only when the new
// behavior is intended.
func SaveBaseline(s Score) error {
	payload := baselineFile{
		Fingerprint:   s.Fingerprint,
		Passed:        s.Passed,
		Failed:        s.Failed,
		Skipped:       s.Skipped,
		Total:         s.Total,
		Allocs:        s.TotalAllocs,
		AllocBytes:    s.TotalAllocBytes,
		CompileAllocs: s.TotalCompileAllocs,
		RunAllocs:     s.TotalRunAllocs,
		TolerancePct:  BaselineTolerancePct,
		SavedAt:       time.Now().UTC(),
		SavedHost:     currentHostSignature(),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(baselinePath, data, 0o600)
}

func loadBaseline() (baselineFile, bool) {
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		return baselineFile{}, false
	}
	var b baselineFile
	if err := json.Unmarshal(data, &b); err != nil {
		return baselineFile{}, false
	}
	if b.Fingerprint == "" {
		return baselineFile{}, false
	}
	return b, true
}

// currentHostSignature is the key the baseline uses to decide whether
// a performance comparison is meaningful. It matches HostInfo's
// platform fields but drops the CPU model — the same OS/arch with a
// different CPU is close enough for an allocation comparison.
func currentHostSignature() string {
	h := DetectHost()
	return h.GOOS + "/" + h.GOARCH
}

// ── rankings ────────────────────────────────────────────────────────

func topSlowest(results []Result, n int) []FeatureStat {
	stats := nonSkippedStats(results)
	slices.SortStableFunc(stats, func(a, b FeatureStat) int {
		return cmp.Compare(b.Elapsed, a.Elapsed)
	})
	return takeTop(stats, n)
}

func topAllocators(results []Result, n int) []FeatureStat {
	stats := nonSkippedStats(results)
	slices.SortStableFunc(stats, func(a, b FeatureStat) int {
		return cmp.Compare(b.Allocs, a.Allocs)
	})
	return takeTop(stats, n)
}

func takeTop(stats []FeatureStat, n int) []FeatureStat {
	if len(stats) > n {
		return stats[:n]
	}
	return stats
}

func nonSkippedStats(results []Result) []FeatureStat {
	out := make([]FeatureStat, 0, len(results))
	for i := range results {
		r := &results[i]
		if r.Status == StatusSkip {
			continue
		}
		out = append(out, FeatureStat{
			Name:          r.Name,
			Section:       r.Section,
			Elapsed:       r.Elapsed,
			Allocs:        r.TotalAllocs(),
			CompileAllocs: r.CompileAllocs,
			RunAllocs:     r.RunAllocs,
		})
	}
	return out
}

// ── fingerprint ─────────────────────────────────────────────────────

// fingerprintOf hashes every feature's name and status. Sorting makes
// the hash independent of execution order, so a reordered suite with
// identical outcomes produces the same fingerprint.
//
// The hash is 12 hex characters (48 bits) — enough to distinguish any
// realistic pair of feature sets, short enough to print.
func fingerprintOf(results []Result) string {
	entries := make([]string, 0, len(results))
	for i := range results {
		r := &results[i]
		entries = append(entries, r.Name+"="+statusName(r.Status))
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(sum[:6])
}

func statusName(s Status) string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}
