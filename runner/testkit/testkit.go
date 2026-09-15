// Package testkit provides flow-runner-specific test helpers.
//
// This package is intentionally separate from github.com/nexssp/testkit:
// the generic testkit has no dependency on flow, and this package needs
// one. Keeping them apart means a project that never runs flows does
// not pull flow into its test binaries.
package testkit

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	flowrunner "github.com/nexssp/flow/runner"
)

// Result captures the outcome of one flow run inside a test.
//
// Stdout and Stderr hold the exact bytes the runner would have written
// to the terminal, so a test can assert on the live trace, the metrics
// table, or the banner without spawning a subprocess.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// RunDSL executes a flow body directly, without reading a .flow file.
//
// The DSL is treated as if it were the content of a .flow file: it may
// contain @config, @pipeline, and @assert directives, but not @include
// or @require (those need a real file on disk).
//
// payload is passed as the initial state. libs is the library set the
// DSL may call; pass at least flow.StandardLibrary().
func RunDSL(
	t testing.TB,
	dsl string,
	payload map[string]any,
	libs ...flow.Library,
) Result {
	t.Helper()

	if len(libs) == 0 {
		libs = []flow.Library{flow.StandardLibrary()}
	}

	reg, err := flow.BuildRegistry(libs...)
	if err != nil {
		t.Fatalf("testkit.RunDSL: build registry: %v", err)
	}

	var stdout, stderr bytes.Buffer

	req := flowrunner.Request{
		Path:    writeTempFlow(t, dsl),
		Payload: payload,
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	exit := flowrunner.RunWithRegistry(context.Background(), req, reg, nil)

	return Result{
		ExitCode: exit,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
}

// ExpectSuccess fails the test when the flow did not exit cleanly.
func (r Result) ExpectSuccess(t testing.TB) Result {
	t.Helper()

	if r.ExitCode != 0 {
		t.Fatalf("flow failed with exit code %d\nStdout:\n%s\nStderr:\n%s",
			r.ExitCode, r.Stdout, r.Stderr)
	}

	return r
}

// ExpectFailure fails the test when the flow exited with code 0.
func (r Result) ExpectFailure(t testing.TB) Result {
	t.Helper()

	if r.ExitCode == 0 {
		t.Fatalf("expected flow to fail, but it succeeded\nStdout:\n%s", r.Stdout)
	}

	return r
}

// StdoutContains fails the test when the captured stdout does not
// contain substr. Useful for asserting on live trace lines.
func (r Result) StdoutContains(t testing.TB, substr string) Result {
	t.Helper()

	if !strings.Contains(r.Stdout, substr) {
		t.Fatalf("stdout does not contain %q\nStdout:\n%s", substr, r.Stdout)
	}

	return r
}

func writeTempFlow(t testing.TB, dsl string) string {
	t.Helper()
	dir := t.TempDir()

	path := dir + "/flow.txt"
	if err := writeFile(path, dsl); err != nil {
		t.Fatalf("testkit.RunDSL: write temp flow: %v", err)
	}

	return path
}
