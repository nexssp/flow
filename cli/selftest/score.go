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

// Score summarizes one self-test run in a form that is comparable
// across machines and stable across runs. The allocation totals split
// compile and run phases so a review can distinguish compiler cost
// from runtime cost at the run level, not just per feature.
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

	BaselineFingerprint string `json:"baseline_fingerprint,omitempty"`
	BaselineMatched     bool   `json:"baseline_matched,omitempty"`
	BaselinePresent     bool   `json:"baseline_present,omitempty"`
}

const topN = 5

// Value returns the numeric score in [0, 100]. It is purely the
// fraction of features that passed, formatted to one decimal. A run
// with 66/66 always scores 100.0 on any machine.
func (s Score) Value() float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(s.Passed) / float64(s.Total) * 100
}

// Grade maps the score to a letter. The thresholds are chosen so that
// a single failed feature in a 66-feature suite lands at A (98.5),
// which is what a reviewer expects to see: real, visible, not hidden
// behind an A+.
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

// ComputeScore builds a Score from a completed run's results and
// attaches baseline comparison when the baseline file exists.
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

	if baseline, ok := loadBaseline(); ok {
		s.BaselinePresent = true
		s.BaselineFingerprint = baseline.Fingerprint
		s.BaselineMatched = baseline.Fingerprint == s.Fingerprint
	}
	return s
}

// SaveBaseline writes the current run's fingerprint to the working
// directory. Overwrites any existing baseline; the caller is expected
// to invoke this only when the new behavior is intended.
func SaveBaseline(s Score) error {
	payload := baseline{
		Fingerprint: s.Fingerprint,
		Total:       s.Total,
		Passed:      s.Passed,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(baselinePath, data, 0o600)
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

// ── baseline persistence ────────────────────────────────────────────

const baselinePath = ".nflow_selftest_baseline.json"

type baseline struct {
	Fingerprint string `json:"fingerprint"`
	Total       int    `json:"total"`
	Passed      int    `json:"passed"`
}

func loadBaseline() (baseline, bool) {
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		return baseline{}, false
	}
	var b baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return baseline{}, false
	}
	return b, b.Fingerprint != ""
}
