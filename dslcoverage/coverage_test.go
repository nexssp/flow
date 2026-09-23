package coverage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/flow"
	flowrunner "github.com/nexssp/flow/runner"
	"github.com/nexssp/flow/runner/capability"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

// TestCoverage walks the coverage tree and runs every .nflow file that is
// not a helper or a service definition.
//
// Rules (documented in README.md):
//
//   - A file whose name ends in `_helper.nflow` is a fixture, not a test.
//   - A file whose name ends in `_service.nflow` is a service definition,
//     not run in run-mode.
//   - Any other .nflow is executed through the runner.
//   - Every test file must declare at least one `@assert:`.
//   - Any file whose name begins with `skip_` is skipped.
//
// Each file gets a fresh registry. This is what allows two files to
// declare a directive of the same name (e.g. `@parallel coverage_parallel`)
// without colliding: directives contribute actions to the registry they
// are mounted on, and each file mounts a private copy.
//
// The runner does its own preprocess → materialize → contribute →
// register-pipelines chain internally. The test does not duplicate that
// chain; it only inspects the file once to enforce the "at least one
// @assert" rule, then hands the path to the runner and lets it do the
// real work. Duplicating the chain was a source of subtle drift between
// what the test exercised and what production actually ran.
func TestCoverage(t *testing.T) {
	root := "."
	if _, err := os.Stat("base"); err != nil {
		root = filepath.Join("testdata", "coverage")
	}

	// Ensure baseline exists for stdlib/bench_compare.nflow during test run,
	// then delete it immediately in Cleanup so no artifacts linger in git.
	benchFile := filepath.Join(root, "coverage_bench.json")
	if _, err := os.Stat(benchFile); os.IsNotExist(err) {
		baselineData := []byte(`{"action":"cov.echo","iterations":50,"min_ms":0.1,"max_ms":5.0,"mean_ms":0.5,"p50_ms":0.4,"p95_ms":1.0,"p99_ms":2.0,"rps":50000,"elapsed_ms":10}`)
		_ = os.WriteFile(benchFile, baselineData, 0o600)
	}
	t.Cleanup(func() {
		_ = os.Remove(benchFile)
	})

	files := collectFlowFiles(t, root)
	if len(files) == 0 {
		t.Fatalf("no coverage files found under %s", root)
	}
	t.Logf("discovered %d coverage file(s)", len(files))

	for _, path := range files {
		path := path
		name, _ := filepath.Rel(root, path)

		t.Run(name, func(t *testing.T) {
			reg := buildCoverageRegistry(t)
			runOne(t, reg, path)
		})
	}
}

// collectFlowFiles walks root and returns every executable .nflow file,
// in lexical order for deterministic subtest ordering.
func collectFlowFiles(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".nflow") {
			return nil
		}
		if isExcludedFile(d.Name()) {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	return out
}

// isExcludedFile reports whether a file is a fixture, a service
// declaration, or a parked failure, and therefore not executed.
func isExcludedFile(name string) bool {
	switch {
	case strings.HasPrefix(name, "skip_"):
		return true
	case strings.HasSuffix(name, "_helper.nflow"):
		return true
	case strings.HasSuffix(name, "_service.nflow"):
		return true
	default:
		return false
	}
}

// buildCoverageRegistry registers the standard library plus every harness
// action the coverage files depend on. Harness actions are deliberately
// small and named `cov.*` so they cannot collide with real actions.
func buildCoverageRegistry(t *testing.T) *action.Registry {
	t.Helper()

	reg, err := action.NewRegistry(
		flow.BaseLibrary(),
		flow.StandardLibrary(),
		action.Library{Name: "coverage", Actions: coverageActions()},
	)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return reg
}

// runOne runs a single .nflow file through the shipped runner in test mode.
//
// Output is buffered, not streamed. On failure both buffers are attached
// to the subtest via t.Logf, so `go test -run TestCoverage/directives/include`
// shows the exact runner trace that produced the non-zero exit.
//
// The function is deliberately thin:
//
//  1. Preprocess once, only to enforce the "at least one @assert" rule
//     and to fail fast on a preprocess error with the file's own message.
//  2. Set up the security scope so files that exercise :role=, :perm=,
//     and :feature= can resolve their guards.
//  3. Hand the path to RunWithRegistry, which owns the rest.
//
// Do not duplicate the runner's preprocess/materialize/contribute chain
// here. If those steps need to change, they change in one place.
func runOne(t *testing.T, reg *action.Registry, path string) {
	t.Helper()

	pre, err := flow.Preprocess(path)
	if err != nil {
		t.Fatalf("preprocess %s: %v", path, err)
	}
	if len(pre.Asserts) == 0 {
		t.Fatalf("%s declares no @assert: directives", path)
	}

	resolver, err := capability.NewResolver(context.Background(), reg, pre.DSL)
	if err != nil {
		t.Fatalf("capability resolver %s: %v", path, err)
	}
	if resolver != nil {
		defer resolver.Close()
	}

	parent := t.Context()
	ctx, scope, release := xctx.NewScope(parent)
	defer release()

	scope.Roles = []string{"admin", "reviewer"}
	scope.Permissions = []string{"write", "*"}
	scope.Features = []string{"coverage_enabled", "all"}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer

	exit := flowrunner.RunWithRegistry(
		ctx,
		flowrunner.Request{
			Path:    path,
			Payload: map[string]any{},
			Args:    []string{"--approval=none", "--approve=coverage_approval"},
			Stdout:  &stdout,
			Stderr:  &stderr,
			Store:   flowrunner.NewMemoryCheckpointStore(),
		},
		reg,
		nil,
	)

	if exit != 0 {
		t.Logf("--- stdout ---\n%s", stdout.String())
		t.Logf("--- stderr ---\n%s", stderr.String())
		t.Fatalf("flow %s failed with exit code %d", path, exit)
	}
}
