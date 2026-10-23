package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	xtest "github.com/nexssp/kernel/xtest/ktest"
)

func TestParseSelfBenchArgs(t *testing.T) {
	parsed, err := parseSelfBenchArgs([]string{"API", "--iterations", "7", "parallel", "--warmup=0", "--json", "--path", "some folder"}, io.Discard)
	xtest.RequireNoError(t, err)
	xtest.RequireEqual(t, parsed.filters, []string{"API", "parallel"})
	xtest.RequireEqual(t, parsed.path, "some folder")
	xtest.RequireEqual(t, parsed.options.Iterations, 7)
	xtest.RequireEqual(t, parsed.options.Warmup, 0)
	xtest.RequireEqual(t, parsed.options.JSON, true)
	defaults, err := parseSelfBenchArgs(nil, io.Discard)
	xtest.RequireNoError(t, err)
	xtest.RequireEqual(t, defaults.options.Iterations, 50)
	xtest.RequireEqual(t, defaults.options.Warmup, 3)
	for _, args := range [][]string{{"--iterations", "0"}, {"--iterations=-1"}, {"--iterations=1000001"}, {"--warmup=-1"}, {"--warmup=1000001"}, {"--iterations=bad"}, {"--path"}, {"--unknown"}} {
		_, err := parseSelfBenchArgs(args, io.Discard)
		xtest.RequireCondition(t, err != nil, "invalid flags must fail")
	}
}

func TestSelfBenchDispatch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "custom.nflow")
	xtest.RequireNoError(t, os.WriteFile(file, []byte("{ value: 42 }"), 0o600))
	for _, args := range [][]string{
		{"self", "bench", "--list", "--json"},
		{"self", "bench", "--path", file, "custom", "--iterations=2", "--warmup=0", "--json"},
		{"self", "bench", "--path", dir, "--iterations=1", "--warmup=0"},
	} {
		code := -1
		content, _ := captureIO(t, func() { code = RunContext(context.Background(), args) })
		xtest.RequireEqual(t, code, 0)
		xtest.RequireCondition(t, content != "", "must render output")
		if bytes.Contains([]byte(args[len(args)-1]), []byte("json")) {
			xtest.RequireCondition(t, json.Valid([]byte(content)), "CLI stdout must be valid JSON")
		}
	}
}

func TestSelfBenchJSONRedirectsScenarioOutput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "print.nflow")
	xtest.RequireNoError(t, os.WriteFile(file, []byte(`{ value: "scenario-noise", stream: "stdout" } -> runtime.print`), 0o600))
	code := -1
	stdout, stderr := captureIO(t, func() {
		code = RunContext(context.Background(), []string{"self", "bench", "--path", file, "--iterations=1", "--warmup=0", "--json"})
	})
	xtest.RequireEqual(t, code, 0)
	xtest.RequireCondition(t, json.Valid([]byte(stdout)), "scenario output must not corrupt JSON")
	xtest.RequireStringContains(t, stderr, "scenario-noise")
}
