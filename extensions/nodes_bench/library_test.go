package nodes_bench

import (
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresLibrary(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

func TestLibrary_ActionNames(t *testing.T) {
	t.Parallel()
	want := map[string]bool{"bench.run": false, "bench.save": false, "bench.compare": false}
	for _, a := range Library().Actions {
		if _, ok := want[a.Describe().Name]; ok {
			want[a.Describe().Name] = true
		}
	}
	for name, seen := range want {
		ktest.RequireCondition(t, seen, "action %q missing", name)
	}
}

func TestPercentile(t *testing.T) {
	t.Parallel()
	samples := []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	durations := make([]time.Duration, len(samples))
	for i, v := range samples {
		durations[i] = time.Duration(v)
	}

	// Nearest-rank percentile over 10 samples: index = int(n*p) - 1,
	// clamped to [0, n-1]. p=0.50 → index 4 → value 50.
	cases := []struct {
		p    float64
		want int64
	}{
		{0.50, 50},
		{0.95, 90},
		{0.99, 90},
	}
	for _, c := range cases {
		got := percentile(durations, c.p)
		ktest.RequireEqual(t, int64(got), c.want)
	}
}

func TestPercentile_Empty(t *testing.T) {
	t.Parallel()
	ktest.RequireEqual(t, percentile(nil, 0.5), time.Duration(0))
}

func TestCompareWorseIfHigher(t *testing.T) {
	t.Parallel()
	metrics := map[string]Metric{}
	var regressions []string

	compareWorseIfHigher(metrics, &regressions, "p95_ms", 100, 110, 5)
	ktest.RequireCondition(t, metrics["p95_ms"].Regressed, "expected regression")
	ktest.RequireEqual(t, len(regressions), 1)

	metrics = map[string]Metric{}
	regressions = nil
	compareWorseIfHigher(metrics, &regressions, "p95_ms", 100, 103, 5)
	ktest.RequireCondition(t, !metrics["p95_ms"].Regressed, "should not regress within tolerance")
}

func TestCompareWorseIfLower(t *testing.T) {
	t.Parallel()
	metrics := map[string]Metric{}
	var regressions []string

	compareWorseIfLower(metrics, &regressions, "rps", 1000, 900, 5)
	ktest.RequireCondition(t, metrics["rps"].Regressed, "expected rps regression")
}
