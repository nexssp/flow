package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestHarnessTestCommandSkipsOnlyNFlowConformanceWhenRequested(t *testing.T) {
	t.Setenv(skipTestAllNFlowConformanceEnv, "")
	ktest.RequireEqual(t, newHarnessTestCommand(context.Background()).Args, []string{
		"go", "test", "-json", "-count=1", "./builder", "./cli/...",
	})

	t.Setenv(skipTestAllNFlowConformanceEnv, "1")
	ktest.RequireEqual(t, newHarnessTestCommand(context.Background()).Args, []string{
		"go", "test", "-json", "-count=1", "-skip=" + testAllNFlowConformanceSkipRegex, "./builder", "./cli/...",
	})
}

func TestFailureSummaryShowsFailingTestWithoutPassingNoise(t *testing.T) {
	events := []testEvent{
		{Action: "output", Package: "example/pkg", Test: "TestPassing", Output: "noisy passing-test output\n"},
		{Action: "pass", Package: "example/pkg", Test: "TestPassing", Elapsed: 0.01},
		{Action: "run", Package: "example/pkg", Test: "TestBroken"},
		{Action: "output", Package: "example/pkg", Test: "TestBroken", Output: "assertion failed: expected 2, got 1\n"},
		{Action: "fail", Package: "example/pkg", Test: "TestBroken", Elapsed: 0.25},
		{Action: "fail", Package: "example/pkg", Elapsed: 0.3},
	}

	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	for _, event := range events {
		ktest.RequireNoError(t, encoder.Encode(event))
	}
	report, err := parseTestEvents(&input)
	ktest.RequireNoError(t, err)

	summary := renderFailureSummary(report, 1, "/tmp/flow-test-harness.jsonl")
	ktest.RequireStringContains(t, summary, "1 failing test(s), 0 package/build failure(s)")
	ktest.RequireStringContains(t, summary, "example/pkg — TestBroken")
	ktest.RequireStringContains(t, summary, "assertion failed: expected 2, got 1")
	ktest.RequireCondition(t, !strings.Contains(summary, "noisy passing-test output"), "summary included output from a passing test")
}

func TestTestLogPathUsesRunnerTemp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNNER_TEMP", dir)
	ktest.RequireEqual(t, testLogPath(), filepath.Join(dir, "flow-test-harness.jsonl"))
}

func TestConformanceFailureSummaryExtractsFeatureAndCause(t *testing.T) {
	reportJSON := `{"failed":1,"sections":[{"name":"macros","features":[{"name":"Argument-map composition and readable map entries","source":"macros/nflows/argument_map_composition.nflow","status":"failed","error":"assert result.tree_only == \\\"true\\\" failed (got false)"}]}]}`
	details := conformanceFailureSummary([]string{
		"NFlow conformance self-test failed (exit 1):",
		reportJSON,
	})
	joined := strings.Join(details, "\n")
	ktest.RequireStringContains(t, joined, "Argument-map composition and readable map entries")
	ktest.RequireStringContains(t, joined, "argument_map_composition.nflow")
	ktest.RequireStringContains(t, joined, "got false")
}

func TestPackageBuildFailureIncludesCompilerError(t *testing.T) {
	input := `{"Action":"output","Package":"example/bad","Output":"# example/bad\nexample/bad/file.go:3:2: undefined: missingSymbol\n"}` + "\n" + `{"Action":"fail","Package":"example/bad"}`
	report, err := parseTestEvents(strings.NewReader(input))
	ktest.RequireNoError(t, err)

	summary := renderFailureSummary(report, 1, "/tmp/flow-test-harness.jsonl")
	ktest.RequireStringContains(t, summary, "0 failing test(s), 1 package/build failure(s)")
	ktest.RequireStringContains(t, summary, "package/build failure")
	ktest.RequireStringContains(t, summary, "undefined: missingSymbol")
}
