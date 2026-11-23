package runner_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nexssp/flow/runner"
)

func TestRunSourceMarksActionAssertionsUnavailableAfterHistoryTruncation(t *testing.T) {
	cfg := observerTestConfig(t)

	var source strings.Builder
	source.WriteString("@assert: result == \"ok\"\n@assert: len(actions) == 257\n\"ok\"")
	for range 257 {
		source.WriteString("\n  -> probe.echo")
	}
	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, source.String(), "action-history-truncated.nflow", nil, runner.Opts{
		Verbosity: 1,
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	if code != 1 {
		t.Fatalf("RunSource returned %d, want an unavailable-action assertion failure; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "✓ result == \"ok\"") {
		t.Fatalf("result-only assertion did not run successfully:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "unavailable: action history exceeded the observer retention limit") {
		t.Fatalf("action-dependent assertion did not report truncated history:\n%s", stderr.String())
	}
}
