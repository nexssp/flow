package selfbench

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	xtest "github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/resilience"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestDiscover(t *testing.T) {
	scenarios, err := Discover("", nil)
	xtest.RequireNoError(t, err)
	xtest.RequireCondition(t, len(scenarios) >= 17, "built-in suite must include common workloads and resilience patterns")
	resilienceScenarios, err := Discover("", []string{"resilience/"})
	xtest.RequireNoError(t, err)
	wantResilience := []string{
		"resilience/baseline.nflow",
		"resilience/cache.nflow",
		"resilience/circuit_breaker.nflow",
		"resilience/coalesce.nflow",
		"resilience/concurrency.nflow",
		"resilience/dedup.nflow",
		"resilience/fallback.nflow",
		"resilience/idempotent.nflow",
		"resilience/race.nflow",
		"resilience/rate_limit.nflow",
		"resilience/retry.nflow",
		"resilience/timeout.nflow",
		"resilience/typed_recovery.nflow",
	}
	xtest.RequireEqual(t, len(resilienceScenarios), len(wantResilience))
	for i, name := range wantResilience {
		xtest.RequireEqual(t, resilienceScenarios[i].Name, name)
	}
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
	bundles := native.SelftestBundles()
	for i := range bundles {
		if bundles[i].ID == resilience.ID {
			bundles[i] = resilience.Bundle(map[string]string{
				"default_max":       "1",
				"backoff":           "constant",
				"min_backoff":       "1ms",
				"max_backoff":       "1ms",
				"retry_all":         "true",
				"failure_threshold": "1",
				"reset_timeout":     "30s",
			})
		}
	}
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
			xtest.RequireCondition(t, r.CompileAllocation.Bytes > 0 && r.CompileAllocation.Allocations > 0,
				"compile phase must report Go heap allocations: %+v", r.CompileAllocation)
			xtest.RequireCondition(t, r.FirstRunAllocation.Bytes > 0 && r.FirstRunAllocation.Allocations > 0,
				"first run must report Go heap allocations: %+v", r.FirstRunAllocation)
			xtest.RequireEqual(t, r.WarmupAllocation.Runs, 1)
			xtest.RequireEqual(t, r.WarmAllocation.Runs, 3)
			xtest.RequireCondition(t, r.WarmAllocation.BytesPerRun > 0 && r.WarmAllocation.AllocationsPerRun > 0,
				"warm runs must report average Go heap allocations: %+v", r.WarmAllocation)
			// Very short phases can fall below the platform clock's resolution.
			xtest.RequireCondition(t, r.CompileMs >= 0 && r.FirstRunMs >= 0 && r.Warm.RPS > 0,
				"phase timings must be non-negative and throughput positive (compile=%vms first-run=%vms rps=%v)", r.CompileMs, r.FirstRunMs, r.Warm.RPS)
		}
	})
}

func TestActionProfileCapturesOnlyMeasuredWarmRuns(t *testing.T) {
	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		actionCalls := 0
		probe := action.New("benchprobe.profile", func(_ context.Context, _ any) (any, error) {
			actionCalls++
			return "ok", nil
		}).Build()
		xtest.RequireNoError(t, cfg.Resolver.Mount(action.Library{Name: "benchprobe", Actions: []action.AnyAction{probe}}))
		scenarios := []Scenario{{Name: "profile.nflow", File: "profile.nflow", Source: "benchprobe.profile"}}
		var output bytes.Buffer
		code := Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 3, Warmup: 2, JSON: true, Profile: true})
		xtest.RequireEqual(t, code, 0)
		var report Report
		xtest.RequireNoError(t, json.Unmarshal(output.Bytes(), &report))
		xtest.RequireEqual(t, report.Profiled, true)
		xtest.RequireEqual(t, actionCalls, 6)
		result := report.Results[0]
		xtest.RequireEqual(t, result.WarmupAllocation.Runs, 2)
		xtest.RequireEqual(t, result.WarmAllocation.Runs, 3)
		var profiled *ActionProfile
		for i := range result.Actions {
			if result.Actions[i].Action == "benchprobe.profile" {
				profiled = &result.Actions[i]
				break
			}
		}
		xtest.RequireCondition(t, profiled != nil, "expected action profile, got %+v", result.Actions)
		xtest.RequireEqual(t, profiled.Calls, 3)
		xtest.RequireEqual(t, profiled.Errors, 0)
		xtest.RequireCondition(t, profiled.TotalMs >= 0 && profiled.SelfMs >= 0 && profiled.MeanMs >= 0 && profiled.MeanSelfMs >= 0,
			"profile durations must be non-negative: %+v", profiled)
	})
}

func TestCPUProfileDoesNotEnableActionProfile(t *testing.T) {
	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		probe := action.New("benchprobe.cpu_profile", func(_ context.Context, _ any) (any, error) {
			return "ok", nil
		}).Build()
		xtest.RequireNoError(t, cfg.Resolver.Mount(action.Library{Name: "benchprobe", Actions: []action.AnyAction{probe}}))
		scenarios := []Scenario{{Name: "cpu-only.nflow", File: "cpu-only.nflow", Source: "benchprobe.cpu_profile"}}
		var output bytes.Buffer
		profilePath := filepath.Join(t.TempDir(), "cpu-only.pprof")
		code := Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 1, Warmup: 0, JSON: true, CPUProfilePath: profilePath})
		xtest.RequireEqual(t, code, 0)
		var report Report
		xtest.RequireNoError(t, json.Unmarshal(output.Bytes(), &report))
		xtest.RequireEqual(t, report.Profiled, false)
		xtest.RequireEqual(t, len(report.Results), 1)
		xtest.RequireEqual(t, len(report.Results[0].Actions), 0)
		xtest.RequireCondition(t, report.Results[0].CPUProfile != nil, "CPU profile summary missing")
		info, err := os.Stat(profilePath)
		xtest.RequireNoError(t, err)
		xtest.RequireCondition(t, info.Size() > 0, "CPU profile is empty")
	})
}

func TestActionProfileCountsRetriesAndFinalErrors(t *testing.T) {
	profiler := NewActionProfiler()
	hook := profiler.Hook()
	profiler.Start()

	retryMeta := &action.Meta{Name: "probe.retry_success"}
	retryCtx, err := hook.Before(context.Background(), nil, retryMeta)
	if err != nil {
		t.Fatal(err)
	}
	hook.OnRetry(retryCtx, nil, 1, xerr.BadRequest("transient"), retryMeta)
	hook.OnRetry(retryCtx, nil, 2, xerr.BadRequest("transient"), retryMeta)
	hook.After(retryCtx, nil, nil, nil, retryMeta)

	errorMeta := &action.Meta{Name: "probe.final_failure"}
	errorCtx, err := hook.Before(context.Background(), nil, errorMeta)
	if err != nil {
		t.Fatal(err)
	}
	hook.After(errorCtx, nil, nil, xerr.BadRequest("final failure"), errorMeta)
	profiler.Stop()

	profiles := make(map[string]ActionProfile, len(profiler.Snapshot()))
	for _, profile := range profiler.Snapshot() {
		profiles[profile.Action] = profile
	}
	retried := profiles["probe.retry_success"]
	if retried.Calls != 1 || retried.Retries != 2 || retried.Errors != 0 {
		t.Fatalf("retry-success counters = %+v", retried)
	}
	failed := profiles["probe.final_failure"]
	if failed.Calls != 1 || failed.Retries != 0 || failed.Errors != 1 {
		t.Fatalf("final-failure counters = %+v", failed)
	}
}

func TestActionProfileBuildsNestedTreeFromNFlowFixture(t *testing.T) {
	scenarios, err := Discover("", []string{"diagnostics/action_tree.nflow"})
	xtest.RequireNoError(t, err)
	xtest.RequireEqual(t, len(scenarios), 1)

	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		var output bytes.Buffer
		code := Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 2, Warmup: 0, JSON: true, Profile: true})
		xtest.RequireEqual(t, code, 0)
		var report Report
		xtest.RequireNoError(t, json.Unmarshal(output.Bytes(), &report))
		profiles := make(map[string]ActionProfile, len(report.Results[0].Actions))
		for _, profile := range report.Results[0].Actions {
			profiles[profile.Action] = profile
		}
		parent, hasParent := profiles["pipeline.diagnostic_parent"]
		child, hasChild := profiles["pipeline.diagnostic_leaf"]
		leaf, hasLeaf := profiles["runtime.noop"]
		xtest.RequireCondition(t, hasParent && hasChild && hasLeaf, "nested fixture actions missing from profile: %+v", report.Results[0].Actions)
		xtest.RequireEqual(t, parent.Parent, "")
		xtest.RequireEqual(t, child.Parent, parent.Path)
		xtest.RequireEqual(t, leaf.Parent, child.Path)
		for _, profile := range []ActionProfile{parent, child, leaf} {
			xtest.RequireEqual(t, profile.Calls, 2)
			xtest.RequireCondition(t, profile.SelfMs >= 0 && profile.SelfMs <= profile.TotalMs,
				"self wall time must be bounded by inclusive wall time: %+v", profile)
		}
	})
}

func TestExclusiveWallTimeSubtractsUnionOfDirectChildren(t *testing.T) {
	start := time.Unix(100, 0)
	end := start.Add(10 * time.Second)
	children := []timeInterval{
		{start: start.Add(2 * time.Second), end: start.Add(6 * time.Second)},
		{start: start.Add(4 * time.Second), end: start.Add(8 * time.Second)},
		{start: start.Add(12 * time.Second), end: start.Add(15 * time.Second)},
	}
	got := exclusiveWallTime(start, end, children)
	if want := 4 * time.Second; got != want {
		t.Fatalf("exclusiveWallTime() = %s, want %s", got, want)
	}
}

func TestJSONNormalizeSelfBenchScenario(t *testing.T) {
	scenarios, err := Discover("", []string{"api/json_normalize.nflow"})
	xtest.RequireNoError(t, err)
	xtest.RequireEqual(t, len(scenarios), 1)

	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		var output bytes.Buffer
		code := Run(ctx, cfg, scenarios, Options{Out: &output, Iterations: 1, Warmup: 0, JSON: true})
		xtest.RequireEqual(t, code, 0)
		var report Report
		xtest.RequireNoError(t, json.Unmarshal(output.Bytes(), &report))
		xtest.RequireEqual(t, len(report.Results), 1)
		xtest.RequireEqual(t, report.Results[0].Error, "")
		xtest.RequireEqual(t, report.Results[0].Warm.Errors, 0)
	})
}

func TestRunFailuresAndList(t *testing.T) {
	withConfig(t, func(ctx context.Context, cfg runner.Config) {
		for _, source := range []string{"no_such_action", "runtime.fail ( message: \"expected\" )", ""} {
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
