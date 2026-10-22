package selftest

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	xtest "github.com/nexssp/kernel/xtest/ktest"
)

func TestCaptureExecutionRestoresAndCloses(t *testing.T) {
	previousStdout, previousStderr, previousLogger := os.Stdout, os.Stderr, slog.Default()
	beforePipes := openPipeFDs(t)
	defer func() {
		os.Stdout = previousStdout
		os.Stderr = previousStderr
		slog.SetDefault(previousLogger)
	}()

	result := captureExecution(func() (any, error) {
		_, _ = os.Stdout.WriteString("stdout marker\n")
		_, _ = os.Stderr.WriteString("stderr marker\n")
		slog.Info("logger marker")
		return "value", nil
	})
	xtest.RequireEqual(t, result.output, any("value"))
	xtest.RequireNoError(t, result.err)
	for _, marker := range []string{"stdout marker", "stderr marker", "logger marker"} {
		xtest.RequireStringContains(t, string(result.captured), marker)
	}
	xtest.RequireEqual(t, os.Stdout, previousStdout)
	xtest.RequireEqual(t, os.Stderr, previousStderr)
	xtest.RequireEqual(t, slog.Default(), previousLogger)
	xtest.RequireEqual(t, openPipeFDs(t), beforePipes)

	wantErr := errors.New("capture callback error")
	result = captureExecution(func() (any, error) { return nil, wantErr })
	xtest.RequireErrorIs(t, result.err, wantErr)
	xtest.RequireEqual(t, os.Stdout, previousStdout)
	xtest.RequireEqual(t, os.Stderr, previousStderr)
	xtest.RequireEqual(t, slog.Default(), previousLogger)
	xtest.RequireEqual(t, openPipeFDs(t), beforePipes)

	var recovered any
	var panicWriter *os.File
	func() {
		defer func() { recovered = recover() }()
		captureExecution(func() (any, error) {
			panicWriter = os.Stdout
			panic("capture callback panic")
		})
	}()
	panicGlobalsRestored := os.Stdout == previousStdout && os.Stderr == previousStderr && slog.Default() == previousLogger
	// Repair the old implementation after recording its failure, so the
	// regression itself cannot leave process-wide streams redirected.
	os.Stdout = previousStdout
	os.Stderr = previousStderr
	slog.SetDefault(previousLogger)
	if panicWriter != nil {
		_ = panicWriter.Close()
	}
	xtest.RequireEqual(t, recovered, any("capture callback panic"))
	xtest.RequireCondition(t, panicGlobalsRestored, "stdout/stderr and slog must be restored after panic")
	xtest.RequireEqual(t, openPipeFDs(t), beforePipes)

	result = captureExecution(func() (any, error) {
		_, _ = os.Stdout.WriteString("after panic")
		return "second capture", nil
	})
	xtest.RequireEqual(t, result.output, any("second capture"))
	xtest.RequireNoError(t, result.err)
	xtest.RequireStringContains(t, string(result.captured), "after panic")
	xtest.RequireEqual(t, os.Stdout, previousStdout)
	xtest.RequireEqual(t, os.Stderr, previousStderr)
	xtest.RequireEqual(t, slog.Default(), previousLogger)
	xtest.RequireEqual(t, openPipeFDs(t), beforePipes)
}

// openPipeFDs snapshots both pipe endpoints on Linux. Without procfs, only
// descriptor assertions are disabled; capture and panic checks still run.
func openPipeFDs(t *testing.T) map[string]int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Logf("pipe descriptor check unavailable without procfs: %v", err)
		return nil
	}
	pipes := make(map[string]int)
	for _, entry := range entries {
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if err == nil && strings.HasPrefix(target, "pipe:[") {
			pipes[target]++
		}
	}
	return pipes
}
