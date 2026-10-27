package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteHarnessModesSeparateThinAndCLIImports(t *testing.T) {
	thinDir := t.TempDir()
	if err := writeHarnessWithMode(thinDir, "", "", "", nil, nil, harnessModeThin); err != nil {
		t.Fatalf("write thin harness: %v", err)
	}
	thinMain, err := os.ReadFile(filepath.Join(thinDir, "main.go"))
	if err != nil {
		t.Fatalf("read thin main: %v", err)
	}
	if strings.Contains(string(thinMain), "github.com/nexssp/flow/cli") {
		t.Fatal("thin harness unexpectedly imports the full CLI package")
	}
	if strings.Contains(string(thinMain), "_ = adopt") {
		t.Fatal("thin harness should not need a dummy adopt reference")
	}
	for _, want := range []string{"github.com/nexssp/flow/runner", "runner.BuildConfig", "runner.RunSource", "fallbackExitCode", "signal.NotifyContext", "syscall.SIGTERM"} {
		if !strings.Contains(string(thinMain), want) {
			t.Errorf("thin main missing %q", want)
		}
	}
	if want := fmt.Sprintf("const fallbackExitCode = %d", HarnessFallbackExitCode); !strings.Contains(string(thinMain), want) {
		t.Fatalf("thin main fallback sentinel mismatch: missing %q", want)
	}

	cliDir := t.TempDir()
	if err := writeHarnessWithMode(cliDir, "", "", "", nil, nil, harnessModeCLI); err != nil {
		t.Fatalf("write CLI harness: %v", err)
	}
	cliMain, err := os.ReadFile(filepath.Join(cliDir, "main.go"))
	if err != nil {
		t.Fatalf("read CLI main: %v", err)
	}
	if !strings.Contains(string(cliMain), "github.com/nexssp/flow/cli") {
		t.Fatal("CLI fallback harness does not import the full CLI package")
	}
}

func TestThinHarnessRefusesUnsupportedInvocationWithFallbackCode(t *testing.T) {
	if testing.Short() {
		t.Skip("generated harness executable test; skipped in short mode")
	}
	root, err := selfModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	driverRoot, driverVersion := resolveDriverInfo()
	buildDir := t.TempDir()
	if err := writeHarnessWithMode(buildDir, driverRoot, driverVersion, "", nil, nil, harnessModeThin, root); err != nil {
		t.Fatalf("write thin harness: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "thin-runner")
	if err := runGoContext(context.Background(), buildDir, "build", "-mod=mod", "-trimpath", "-o", bin, "."); err != nil {
		t.Fatalf("build generated thin harness: %v", err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "unsupported flag", args: []string{"run", "flow.nflow", "--info"}},
		{name: "piped stdin", args: []string{"run", "flow.nflow"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// #nosec G204 -- bin is generated in this test and args are fixed test cases.
			cmd := exec.CommandContext(t.Context(), bin, test.args...)
			cmd.Stdin = strings.NewReader("")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err == nil {
				t.Fatalf("thin runner succeeded; want fallback %d", HarnessFallbackExitCode)
			}
			exitErr, ok := errors.AsType[*exec.ExitError](err)
			if !ok {
				t.Fatalf("thin runner failed without an exit status: %v", err)
			}
			if got := exitErr.ExitCode(); got != HarnessFallbackExitCode {
				t.Fatalf("thin runner exit code = %d, want %d (stdout: %q; stderr: %q)", got, HarnessFallbackExitCode, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCachedHarnessMessageIncludesMode(t *testing.T) {
	for _, mode := range []harnessMode{harnessModeThin, harnessModeCLI} {
		got := cachedHarnessMessage("0123456789abcdef", mode)
		want := "⚡ nflow: using cached runner [01234567:" + string(mode) + "]\n"
		if got != want {
			t.Errorf("cachedHarnessMessage(%q) = %q, want %q", mode, got, want)
		}
	}
}

func TestReportHarnessTimingLabelsPreparationAndCacheHit(t *testing.T) {
	t.Setenv("NFLOW_HARNESS_TIMINGS", "1")
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = w.Close()
		_ = r.Close()
	}()

	reportHarnessTiming(harnessModeThin, "01234567", 12*time.Millisecond, 0, true)
	_ = w.Close()
	os.Stderr = oldStderr
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read timing output: %v", err)
	}
	line := string(output)
	for _, want := range []string{"prepare_and_resolve=12ms", "build=0s", "phase_sum=12ms", "cache_hit=true"} {
		if !strings.Contains(line, want) {
			t.Errorf("timing line missing %q: %s", want, line)
		}
	}
	if strings.Contains(line, " resolve=") || strings.Contains(line, " total=") {
		t.Fatalf("timing line has misleading legacy labels: %s", line)
	}
}
