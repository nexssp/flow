package selfbench

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/nodes_bench"
)

func TestWriteReportHumanIncludesMetricsAndFailures(t *testing.T) {
	report := Report{
		GoVersion:  "go1.23.4",
		OS:         "linux",
		Arch:       "amd64",
		CPUs:       8,
		GOMAXPROCS: 4,
		Results: []Result{
			{
				Name: "ok.nflow", CompileMs: 1.25, FirstRunMs: 2.5, Warmup: 3,
				Warm: nodes_bench.BenchRunRes{Iterations: 5, MinMs: 0.1, MeanMs: 0.2, MaxMs: 0.5, P50Ms: 0.15, P95Ms: 0.4, P99Ms: 0.5, RPS: 123.4},
			},
			{
				Name: "bad.nflow", CompileMs: 3, FirstRunMs: 4, Warmup: 2, Error: "first run: expected failure",
				Warm: nodes_bench.BenchRunRes{Iterations: 7, MinMs: 0.2, MeanMs: 0.3, MaxMs: 0.6, P50Ms: 0.25, P95Ms: 0.5, P99Ms: 0.6, RPS: 45.6, Errors: 1},
			},
		},
	}
	var output bytes.Buffer
	ktest.RequireNoError(t, writeReport(report, Options{Out: &output}))
	text := output.String()
	for _, want := range []string{
		"NEXSS FLOW · SELF BENCH", "compile/setup and first run are in-process, not process cold start; warmup excluded",
		"HOST go1.23.4 · linux/amd64 · 8 CPUs · GOMAXPROCS=4",
		"▸ ok.nflow", "✓ passed", "Compilation", "1.250 ms", "First run", "2.500 ms", "Warmup", "3 runs",
		"Warm samples", "5 samples", "min 0.100 · mean 0.200 · max 0.500 ms",
		"p50 0.150 · p95 0.400 · p99 0.500 ms", "123.4 requests/s", "Sample errors",
		"▸ bad.nflow", "✗ failed", "7 samples", "45.6 requests/s", "1 passed", "1 failed", "(2 total)", "first run: expected failure",
	} {
		ktest.RequireStringContains(t, text, want)
	}
	ktest.RequireEqual(t, false, strings.Contains(text, "\x1b["))
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteReportPropagatesHumanWriterError(t *testing.T) {
	wantErr := errors.New("broken output")
	err := writeReport(Report{}, Options{Out: failingWriter{err: wantErr}})
	ktest.RequireCondition(t, errors.Is(err, wantErr), "must propagate writer failure: %v", err)
}

func TestWriteReportJSONUnchanged(t *testing.T) {
	report := Report{
		Version: 1, GoVersion: "go1", OS: "linux", Arch: "amd64", CPUs: 2, GOMAXPROCS: 2,
		Results: []Result{{
			Name: "ok.nflow", CompileMs: 1, FirstRunMs: 2, Warmup: 3,
			Warm: nodes_bench.BenchRunRes{Action: "ok.nflow", Iterations: 4, Warmup: 3, MinMs: 0.1, MaxMs: 0.5, MeanMs: 0.3, P50Ms: 0.2, P95Ms: 0.5, P99Ms: 0.5, RPS: 10, ElapsedMs: 400},
		}},
	}
	var output bytes.Buffer
	ktest.RequireNoError(t, writeReport(report, Options{Out: &output, JSON: true}))
	const want = `{
  "version": 1,
  "go_version": "go1",
  "os": "linux",
  "arch": "amd64",
  "cpus": 2,
  "gomaxprocs": 2,
  "results": [
    {
      "name": "ok.nflow",
      "compile_ms": 1,
      "first_run_ms": 2,
      "warmup": 3,
      "warm": {
        "action": "ok.nflow",
        "iterations": 4,
        "warmup": 3,
        "errors": 0,
        "min_ms": 0.1,
        "max_ms": 0.5,
        "mean_ms": 0.3,
        "p50_ms": 0.2,
        "p95_ms": 0.5,
        "p99_ms": 0.5,
        "rps": 10,
        "elapsed_ms": 400
      }
    }
  ]
}
`
	ktest.RequireEqual(t, want, output.String())
}
