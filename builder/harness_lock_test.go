package builder

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestHarnessLockStalenessExceedsConfiguredGoTimeout(t *testing.T) {
	t.Setenv("NFLOW_GO_TIMEOUT", "17m")
	if got := goCommandTimeout() + harnessLockStaleGrace; got <= 17*time.Minute {
		t.Fatalf("stale lock threshold = %s, want greater than configured timeout", got)
	}
}

func TestReclaimStaleHarnessLockChecksPIDLiveness(t *testing.T) {
	now := time.Now()
	staleAfter := 15 * time.Minute
	path := filepath.Join(t.TempDir(), "build.lock")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * staleAfter)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if reclaimed := reclaimStaleHarnessLockAt(path, now, staleAfter, func(int) (bool, error) {
		return true, nil
	}); reclaimed {
		t.Fatal("reclaimed a lock whose owner PID is still live")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("live owner's lock was removed: %v", err)
	}
}

func TestReclaimStaleHarnessLockReclaimsConfirmedDeadPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build.lock")
	if err := os.WriteFile(path, []byte("24680\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	if reclaimed := reclaimStaleHarnessLockAt(path, time.Now(), time.Hour, func(pid int) (bool, error) {
		called = true
		if pid != 24680 {
			t.Fatalf("liveness checked PID %d, want 24680", pid)
		}
		return false, nil
	}); !reclaimed {
		t.Fatal("did not reclaim a lock owned by a confirmed-dead PID")
	}
	if !called {
		t.Fatal("did not check the lock owner's PID")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dead owner's lock still exists: stat error %v", err)
	}
}

func TestReclaimMalformedHarnessLockOnlyAfterTimeoutGrace(t *testing.T) {
	now := time.Now()
	staleAfter := 11 * time.Minute
	path := filepath.Join(t.TempDir(), "build.lock")
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reclaimed := reclaimStaleHarnessLockAt(path, now, staleAfter, processAlive); reclaimed {
		t.Fatal("reclaimed a recent malformed lock")
	}

	old := now.Add(-staleAfter - time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if reclaimed := reclaimStaleHarnessLockAt(path, now, staleAfter, processAlive); !reclaimed {
		t.Fatal("did not reclaim an aged malformed lock")
	}
}

func TestReclaimHarnessLockUsesAgeFallbackWhenPIDProbeUnavailable(t *testing.T) {
	now := time.Now()
	staleAfter := 12 * time.Minute
	path := filepath.Join(t.TempDir(), "build.lock")
	if err := os.WriteFile(path, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reclaimed := reclaimStaleHarnessLockAt(path, now, staleAfter, func(int) (bool, error) {
		return false, errProcessLivenessUnavailable
	}); reclaimed {
		t.Fatal("reclaimed a lock before the fallback age threshold")
	}
	old := now.Add(-staleAfter - time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if reclaimed := reclaimStaleHarnessLockAt(path, now, staleAfter, func(int) (bool, error) {
		return false, errProcessLivenessUnavailable
	}); !reclaimed {
		t.Fatal("did not apply the age fallback after the timeout grace")
	}
}
