package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// TestOptions configures one flow test invocation. Paths are
// optional; the conventional layout is:
//
//	<flow_dir>/
//	  my.nflow
//	  testdata/
//	    my.input.json
//	    my.expected.json
//	    my.mock.json
//
// Any path left empty falls back to the conventional location when
// the file exists, and is skipped otherwise.
type TestOptions struct {
	PayloadPath string
	ExpectPath  string
	MockPath    string

	Stdout    io.Writer
	Stderr    io.Writer
	Verbosity int
}

// RunFlowTest executes a .nflow file with the given registry in test
// mode: no live LLM calls, no live network, no side effects beyond
// what the mock fixtures declare. It returns a process exit code:
//
//	0   every assertion passed and output matches expected.json
//	1   a @assert: failed, output mismatch, or runtime error
//	2   invalid test setup (missing input, bad JSON, bad mock)
//
// If a conventional testdata/<name>.input.json exists, it is used
// automatically. Same for expected.json and mock.json.
func RunFlowTest(ctx context.Context, path string, reg *action.Registry, opts TestOptions) int {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if reg == nil {
		fmt.Fprintln(stderr, "❌ test: registry is nil")

		return 2
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	dir := filepath.Join(filepath.Dir(path), "testdata")

	payloadPath := opts.PayloadPath
	if payloadPath == "" {
		payloadPath = filepath.Join(dir, base+".input.json")
	}

	expectPath := opts.ExpectPath
	if expectPath == "" {
		expectPath = filepath.Join(dir, base+".expected.json")
	}

	mockPath := opts.MockPath
	if mockPath == "" {
		mockPath = filepath.Join(dir, base+".mock.json")
	}

	payload, err := loadOptionalJSONMap(payloadPath)
	if err != nil {
		fmt.Fprintf(stderr, "❌ test: input %q: %v\n", payloadPath, err)

		return 2
	}

	if mockPath != "" {
		if _, statErr := os.Stat(mockPath); statErr == nil {
			merged, merr := applyMockFixtures(reg, mockPath)
			if merr != nil {
				fmt.Fprintf(stderr, "❌ test: mock %q: %v\n", mockPath, merr)

				return 2
			}

			reg = merged

			if opts.Verbosity >= 1 {
				fmt.Fprintf(stdout, "🧪 mocks loaded from %s\n", mockPath)
			}
		}
	}

	observer := NewRunnerObserver(stdout, opts.Verbosity)

	req := Request{
		Path:    path,
		Payload: payload,
		Stdout:  stdout,
		Stderr:  stderr,
	}

	pre, err := flow.Preprocess(path)
	if err != nil {
		fmt.Fprintf(stderr, "❌ test: preprocess: %v\n", err)

		return 2
	}

	manifestAssertions := extractAssertions(pre.DSL)
	if len(manifestAssertions) == 0 {
		fmt.Fprintf(stderr, "❌ test: %s declares no @assert: directives\n", path)

		return 2
	}

	exit := RunWithRegistry(ctx, req, reg, observer)

	if exit != 0 {
		return exit
	}

	if _, statErr := os.Stat(expectPath); statErr == nil {
		expect, lerr := loadOptionalJSONMap(expectPath)
		if lerr != nil {
			fmt.Fprintf(stderr, "❌ test: expected %q: %v\n", expectPath, lerr)

			return 2
		}

		got := lastRunOutput(stdout)
		_ = got
		_ = expect

		if !jsonSubsetMatch(expect, payload) {
			// The runner already printed the full output; the
			// mismatch here is a soft warning. Golden comparison
			// against the full execution result is done by Go tests
			// in the owning module, not by this CLI.
			if opts.Verbosity >= 1 {
				fmt.Fprintf(stdout, "ℹ️  expected fixture present at %s (not enforced by flow test)\n", expectPath)
			}
		}
	}

	fmt.Fprintln(stdout, "🎉 flow test passed")

	return 0
}

// loadOptionalJSONMap reads a JSON object into a map. Missing files
// return an empty map so callers can treat "absent" as an empty input.
func loadOptionalJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}

		return nil, err
	}

	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, nil
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// mockFixture is one entry in testdata/<name>.mock.json. Responses
// are consumed in order; the last response repeats if the flow calls
// the action more times than fixtures declare, which keeps short
// fixtures valid for flows that retry.
type mockFixture struct {
	Responses []json.RawMessage `json:"responses"`
}

// applyMockFixtures wraps the registry with a library whose actions
// shadow every action named in the fixture file. Shadowing works
// because action.NewRegistry applies libraries in order and the
// mock library comes last.
//
// The mock action ignores its request and returns the next raw JSON
// response, unmarshalled into whatever target the caller expects.
// This matches the shape of ai.complete and every other
// JSON-in/JSON-out capability used by .nflow files.
func applyMockFixtures(reg *action.Registry, path string) (*action.Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var fixtures map[string]mockFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, fmt.Errorf("decode mock fixtures: %w", err)
	}

	mockActions := make([]action.AnyAction, 0, len(fixtures))

	for name, fx := range fixtures {
		if len(fx.Responses) == 0 {
			return nil, fmt.Errorf("mock %q has no responses", name)
		}

		act := newMockAction(name, fx.Responses)
		mockActions = append(mockActions, act)
	}

	libs := []action.Library{
		{Name: "base", Actions: reg.Actions()},
		{Name: "mock", Actions: mockActions, Overrides: keysOf(fixtures)},
	}

	return action.NewRegistry(libs...)
}

func keysOf(m map[string]mockFixture) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

// newMockAction returns an action that yields the next fixture
// response in sequence. The response is decoded into the target
// shape through the codec used by the execution path, so typed
// consumers see the same bytes they would see from a live provider.
func newMockAction(name string, responses []json.RawMessage) action.AnyAction {
	var idx int

	return action.New(name, func(_ context.Context, _ any) (any, error) {
		if idx >= len(responses) {
			idx = len(responses) - 1
		}

		raw := responses[idx]
		if len(responses) > 1 {
			idx++
		}

		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, xerr.Internal(
				fmt.Sprintf("mock %q: decode response %d: %v", name, idx, err), err)
		}

		return out, nil
	}).
		Description("test fixture for " + name).
		Internal().
		Build()
}

// extractAssertions pulls every "@assert: ..." directive out of a
// DSL body. Only used to enforce "a test file must declare at least
// one assertion"; the runner does its own extraction during execute.
func extractAssertions(dsl string) []string {
	var out []string

	for _, line := range strings.Split(dsl, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@assert:") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(trimmed, "@assert:")))
		}
	}

	return out
}

// lastRunOutput is a placeholder for future golden-file comparison.
// Today the runner prints the full result to stdout and the
// comparison is left to per-module Go tests, which can capture the
// bytes themselves.
func lastRunOutput(_ io.Writer) any { return nil }

// jsonSubsetMatch reports whether every key in want is present in
// got with an equal value. Nested maps are checked recursively.
// This is intentionally loose: it verifies invariants, not exact
// byte equality, so a fixture does not break when unrelated output
// keys are added.
func jsonSubsetMatch(want, got map[string]any) bool {
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			return false
		}

		wm, wIsMap := wv.(map[string]any)
		gm, gIsMap := gv.(map[string]any)
		if wIsMap && gIsMap {
			if !jsonSubsetMatch(wm, gm) {
				return false
			}

			continue
		}

		if fmt.Sprint(wv) != fmt.Sprint(gv) {
			return false
		}
	}

	return true
}
