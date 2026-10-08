package cli

import (
	"bytes"
	"os"
	"sync"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

// captureMutex serializes tests that reassign the process-global
// os.Stdout and os.Stderr. The reassignment itself is the race: two
// parallel tests swapping the same globals will see each other's
// pipes, and the race detector flags the read/write pair even though
// the code under test is correct. Holding this mutex for the full
// swap window eliminates the race without serializing the code under
// test — the code still runs in parallel, only the capture window is
// serialized.
var captureMutex sync.Mutex

// captureIO runs fn with os.Stdout and os.Stderr redirected to
// in-memory buffers.
//
// On success the buffers are discarded — the test's log stays clean.
// If fn calls t.Fatal or the test is run with -v, the captured
// streams are attached to the test's log so the failure can be
// debugged without a second run.
//
// fn must not call t.Parallel. Tests that want to see the captured
// output for assertions should use captureIO and read its return
// values directly instead of relying on this side effect.
func captureIO(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	captureMutex.Lock()
	defer captureMutex.Unlock()

	origOut := os.Stdout
	origErr := os.Stderr

	outR, outW, err := os.Pipe()
	ktest.RequireNoError(t, err)
	errR, errW, err := os.Pipe()
	ktest.RequireNoError(t, err)

	os.Stdout = outW
	os.Stderr = errW

	var wg sync.WaitGroup
	wg.Add(2)

	var outBuf, errBuf bytes.Buffer
	go func() {
		defer wg.Done()
		_, _ = outBuf.ReadFrom(outR)
	}()
	go func() {
		defer wg.Done()
		_, _ = errBuf.ReadFrom(errR)
	}()

	var cleanedUp bool
	cleanup := func() {
		if cleanedUp {
			return
		}
		cleanedUp = true

		_ = outW.Close()
		_ = errW.Close()
		os.Stdout = origOut
		os.Stderr = origErr
		wg.Wait()
		_ = outR.Close()
		_ = errR.Close()

		if outBuf.Len() > 0 {
			t.Logf("captured stdout:\n%s", outBuf.String())
		}
		if errBuf.Len() > 0 {
			t.Logf("captured stderr:\n%s", errBuf.String())
		}
	}
	defer cleanup()

	fn()

	// Przed odczytem z buforów musimy zamknąć rury i poczekać na zakończenie goroutines!
	cleanup()

	return outBuf.String(), errBuf.String()
}
