package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAcquireHarnessLockRefreshesPIDMetadata(t *testing.T) {
	dir := t.TempDir()
	unlock, err := acquireHarnessLock(context.Background(), dir)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer unlock()
	data, err := os.ReadFile(filepath.Join(dir, "build.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != strconv.Itoa(os.Getpid())+"\n" {
		t.Fatalf("lock metadata = %q, want current PID", got)
	}
}

func TestAcquireHarnessLockSerializesCallers(t *testing.T) {
	dir := t.TempDir()
	firstUnlock, err := acquireHarnessLock(context.Background(), dir)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer firstUnlock()

	file, err := os.OpenFile(filepath.Join(dir, "build.lock"), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, supported, err := lockHarnessFile(file); !supported {
		t.Skip("platform does not provide advisory file locks")
	} else if err == nil {
		t.Fatal("second lock attempt unexpectedly acquired the held lock")
	} else if !harnessLockBusy(err) {
		t.Fatalf("second lock attempt failed for an unexpected reason: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireHarnessLock(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter error = %v, want context.Canceled", err)
	}

	firstUnlock()
	secondUnlock, err := acquireHarnessLock(context.Background(), dir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	secondUnlock()
	if _, err := os.Stat(filepath.Join(dir, "build.lock")); err != nil {
		t.Fatalf("persistent lock file missing after release: %v", err)
	}
}

func TestAcquireHarnessLockDoesNotAcquireWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if unlock, err := acquireHarnessLock(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("canceled acquisition = %v, want context.Canceled", err)
	}
}

func TestHarnessLockReleaseDoesNotDeleteReplacementPath(t *testing.T) {
	dir := t.TempDir()
	unlock, err := acquireHarnessLock(context.Background(), dir)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	path := filepath.Join(dir, "build.lock")
	if err := os.Remove(path); err != nil {
		t.Fatalf("replace lock path: %v", err)
	}
	if err := os.WriteFile(path, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("old handle removed replacement path: %v", err)
	}
	if string(data) != "replacement\n" {
		t.Fatalf("replacement content = %q, want unchanged", data)
	}
}

func TestAcquireHarnessLockWaitsUntilReleased(t *testing.T) {
	dir := t.TempDir()
	firstUnlock, err := acquireHarnessLock(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	firstReleased := false
	releaseFirst := func() {
		if !firstReleased {
			firstUnlock()
			firstReleased = true
		}
	}
	defer releaseFirst()
	busy := make(chan struct{}, 1)
	result := make(chan error, 1)
	go func() {
		unlock, err := acquireHarnessLockWithBusyObserver(context.Background(), dir, func() {
			select {
			case busy <- struct{}{}:
			default:
			}
		})
		if err == nil {
			unlock()
		}
		result <- err
	}()

	// The independently opened descriptor confirms contention before release.
	file, err := os.OpenFile(filepath.Join(dir, "build.lock"), os.O_RDWR, 0o600)
	if err != nil {
		releaseFirst()
		t.Fatal(err)
	}
	_, supported, lockErr := lockHarnessFile(file)
	_ = file.Close()
	if !supported {
		t.Skip("platform does not provide advisory file locks")
	}
	if lockErr == nil || !harnessLockBusy(lockErr) {
		t.Fatalf("expected busy lock, got %v", lockErr)
	}
	select {
	case <-busy:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not observe the held lock")
	}
	releaseFirst()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("second caller failed after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second caller did not acquire after release")
	}
}

func TestAcquireHarnessLockCancellationWhileWaiting(t *testing.T) {
	dir := t.TempDir()
	firstUnlock, err := acquireHarnessLock(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer firstUnlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	busy := make(chan struct{}, 1)
	result := make(chan error, 1)
	go func() {
		unlock, err := acquireHarnessLockWithBusyObserver(ctx, dir, func() {
			select {
			case busy <- struct{}{}:
			default:
			}
		})
		if err == nil {
			unlock()
		}
		result <- err
	}()
	select {
	case <-busy:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not observe the held lock")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not stop after cancellation")
	}
}
