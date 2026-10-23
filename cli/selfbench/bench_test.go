package selfbench

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	xtest "github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestDiscover(t *testing.T) {
	scenarios, err := Discover("", nil)
	xtest.RequireNoError(t, err)
	xtest.RequireCondition(t, len(scenarios) >= 4, "built-in suite must include common workloads")
	filtered, err := Discover("", []string{"API", "parallel"})
	xtest.RequireNoError(t, err)
	xtest.RequireCondition(t, len(filtered) >= 2, "filters should select API and parallel scenarios")
	_, err = Discover("", []string{"not-a-scenario"})
	xtest.RequireErrorContains(t, err, "no scenarios matched")
	dir := t.TempDir()
	xtest.RequireNoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
	file := filepath.Join(dir, "nested", "new.nflow")
	xtest.RequireNoError(t, os.WriteFile(file, []byte("{ value: 42 }"), 0o600))
	xtest.RequireNoError(t, os.WriteFile(filepath.Join(dir, "ignore.json"), []byte("{}"), 0o600))
	scenarios, err = Discover(dir, nil)
	xtest.RequireNoError(t, err)
	xtest.RequireEqual(t, len(scenarios), 1)
	xtest.RequireEqual(t, scenarios[0].Name, "nested/new.nflow")
	scenarios, err = Discover(file, nil)
	xtest.RequireNoError(t, err)
	xtest.RequireEqual(t, scenarios[0].Name, "new.nflow")
	_, err = Discover(filepath.Join(dir, "ignore.json"), nil)
	xtest.RequireErrorContains(t, err, ".nflow")
	_, err = Discover(filepath.Join(dir, "missing"), nil)
	xtest.RequireErrorContains(t, err, "path")
}

func TestDiscoverRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.nflow")
	xtest.RequireNoError(t, os.WriteFile(outside, []byte("runtime.noop"), 0o600))
	link := filepath.Join(dir, "link.nflow")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Discover(dir, nil)
	xtest.RequireErrorContains(t, err, "symlink")
	_, err = Discover(link, nil)
	xtest.RequireErrorContains(t, err, "symlink")
}

func withConfig(t *testing.T, fn func(context.Context, runner.Config)) {
	t.Helper()
	bundles := native.Bundles()
	host := runner.NewHost()
	xtest.RequireNoError(t, host.OwnAll(bundles))
	xtest.RequireNoError(t, host.Run(context.Background(), func(ctx context.Context) error {
		cfg, err := runner.BuildConfig(bundles)
		xtest.RequireNoError(t, err)
		fn(ctx, cfg)
		return nil
	}))
}

func TestBuiltinScenarios(t *testing.T) {
	scenarios, err := Discover("", nil)
	xtest.RequireNoError(t, err)
	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		for _, scenario := range scenarios {
			t.Run(scenario.Name, func(t *testing.T) {
				execution, err := runner.Execute(ctx, cfg, scenario.Source, scenario.File, map[string]any{})
				xtest.RequireNoError(t, err)
				assertions, _ := execution.Meta["asserts"].([]string)
				xtest.RequireCondition(t, len(assertions) > 0, "scenario must declare expected results")
				var output bytes.Buffer
				code := runner.RunAssertionsContext(ctx, &output, execution.Output, 0, nil, assertions, 0)
				if code != 0 {
					t.Fatalf("scenario assertions: %s", output.String())
				}
			})
		}
		var output bytes.Buffer
		xtest.RequireEqual(t, Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 3, Warmup: 1, JSON: true}), 0)
		var report Report
		xtest.RequireNoError(t, json.Unmarshal(output.Bytes(), &report))
		xtest.RequireEqual(t, len(report.Results), len(scenarios))
		for _, r := range report.Results {
			xtest.RequireEqual(t, r.Warm.Iterations, 3)
			xtest.RequireEqual(t, r.Warm.Warmup, 1)
			xtest.RequireEqual(t, r.Warm.Errors, 0)
			xtest.RequireCondition(t, r.CompileMs > 0 && r.FirstRunMs > 0 && r.Warm.RPS > 0, "phase timings and throughput must be measured")
		}
	})
}

func TestRunFailuresAndList(t *testing.T) {
	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		for _, source := range []string{"no_such_action", "runtime.fail @{ message: \"expected\" }", ""} {
			var output bytes.Buffer
			code := Run(ctx, cfg, []Scenario{{Name: "bad.nflow", File: "bad.nflow", Source: source}}, Options{Out: &output, Iterations: 1, JSON: true})
			xtest.RequireEqual(t, code, 1)
			var report Report
			xtest.RequireNoError(t, json.Unmarshal(output.Bytes(), &report))
			xtest.RequireCondition(t, report.Results[0].Error != "", "must report execution error")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		var output bytes.Buffer
		scenarios := []Scenario{{Name: "never.nflow", Source: "runtime.noop"}}
		xtest.RequireEqual(t, Run(canceled, cfg, scenarios, Options{Out: &output, Iterations: 1, JSON: true}), 1)
		output.Reset()
		xtest.RequireEqual(t, Run(ctx, runner.Config{}, scenarios, Options{Out: &output, Iterations: 1, List: true, JSON: true}), 0)
		xtest.RequireStringContains(t, output.String(), "never.nflow")
		xtest.RequireEqual(t, Run(ctx, cfg, scenarios, Options{Iterations: 0}), 2)
		xtest.RequireEqual(t, Run(ctx, cfg, scenarios, Options{Iterations: 1, Warmup: -1}), 2)
	})
}

func TestMeasurementCallCountsAndWarmupFailure(t *testing.T) {
	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		calls := 0
		failAt := 0
		counter := action.New("benchprobe.count", func(_ context.Context, _ any) (any, error) {
			calls++
			if calls == failAt {
				return nil, xerr.BadRequest("probe failure")
			}
			return calls, nil
		}).Build()
		xtest.RequireNoError(t, cfg.Resolver.Mount(action.Library{Name: "benchprobe", Actions: []action.AnyAction{counter}}))
		scenarios := []Scenario{{Name: "count.nflow", File: "count.nflow", Source: "benchprobe.count"}}
		var output bytes.Buffer
		xtest.RequireEqual(t, Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 5, Warmup: 2, JSON: true}), 0)
		xtest.RequireEqual(t, calls, 8)
		calls = 0
		failAt = 2
		output.Reset()
		xtest.RequireEqual(t, Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 5, Warmup: 2, JSON: true}), 1)
		xtest.RequireEqual(t, calls, 2)
		xtest.RequireStringContains(t, output.String(), "warmup:")
		calls = 0
		failAt = 4
		output.Reset()
		xtest.RequireEqual(t, Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 5, Warmup: 2, JSON: true}), 1)
		var report Report
		xtest.RequireNoError(t, json.Unmarshal(output.Bytes(), &report))
		xtest.RequireEqual(t, report.Results[0].Warm.Errors, 1)
		xtest.RequireEqual(t, calls, 8)
	})
}
