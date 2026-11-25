package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newListShowTestInvocation(t *testing.T) *invocation {
	t.Helper()
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatalf("newNativeInvocation() error = %v", err)
	}
	return inv
}

func TestListAcceptsOptionsAfterPattern(t *testing.T) {
	inv := newListShowTestInvocation(t)

	var code int
	stdout, _ := captureIO(t, func() {
		code = runListInProcess(context.Background(), inv, []string{"fs", "--json"})
	})
	if code != 0 {
		t.Fatalf("list with trailing --json code = %d, want 0", code)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("list with trailing --json did not emit JSON:\n%s", stdout)
	}

	stdout, _ = captureIO(t, func() {
		code = runListInProcess(context.Background(), inv, []string{"fs", "--no-color"})
	})
	if code != 0 {
		t.Fatalf("list with trailing --no-color code = %d, want 0; output: %s", code, stdout)
	}
}

func TestListAcceptsTrailingFlowOptionAndRejectsUnknownOption(t *testing.T) {
	inv := newListShowTestInvocation(t)
	flowPath := writeLifecycleFlow(t, "\"ready\"\n")

	var code int
	stdout, stderr := captureIO(t, func() {
		code = runList(context.Background(), inv, []string{"fs", "--flow", flowPath, "--json"})
	})
	if code != 0 {
		t.Fatalf("list with trailing --flow FILE code = %d, want 0; stdout: %s; stderr: %s", code, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("list with trailing --flow FILE --json did not emit JSON:\n%s", stdout)
	}

	_, stderr = captureIO(t, func() {
		code = runListInProcess(context.Background(), inv, []string{"fs", "--unknown"})
	})
	if code != 2 || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("list with trailing unknown option code=%d stderr=%q; want code 2 and unknown-flag diagnostic", code, stderr)
	}
}

func TestShowAcceptsOptionsAfterTargetAndRequiresOneTarget(t *testing.T) {
	inv := newListShowTestInvocation(t)

	var code int
	stdout, _ := captureIO(t, func() {
		code = runShowInProcess(context.Background(), inv, []string{"example", "--no-color"})
	})
	if code != 0 {
		t.Fatalf("show with trailing --no-color code = %d, want 0; output: %s", code, stdout)
	}

	_, stderr := captureIO(t, func() {
		code = runShowInProcess(context.Background(), inv, []string{"example", "--unknown"})
	})
	if code != 2 || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("show with trailing unknown option code=%d stderr=%q; want code 2 and unknown-flag diagnostic", code, stderr)
	}

	_, stderr = captureIO(t, func() {
		code = runShowInProcess(context.Background(), inv, []string{"example", "second"})
	})
	if code != 2 || !strings.Contains(stderr, "usage: nflow show <name>") {
		t.Fatalf("show with two targets code=%d stderr=%q; want code 2 and usage diagnostic", code, stderr)
	}
}

func TestShowAcceptsTrailingFlowOption(t *testing.T) {
	inv := newListShowTestInvocation(t)
	flowPath := writeLifecycleFlow(t, "\"ready\"\n")

	var code int
	stdout, stderr := captureIO(t, func() {
		code = runShow(context.Background(), inv, []string{"example", "--flow", flowPath, "--no-color"})
	})
	if code != 0 {
		t.Fatalf("show with trailing --flow FILE code = %d, want 0; stdout: %s; stderr: %s", code, stdout, stderr)
	}
}

func TestShowDoesNotScanFlowOptionAfterDoubleDash(t *testing.T) {
	inv := newListShowTestInvocation(t)

	var code int
	_, stderr := captureIO(t, func() {
		code = runShow(context.Background(), inv, []string{"--", "--flow=missing.nflow"})
	})
	if code == 0 {
		t.Fatal("show with a missing target unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `no atom, modifier, directive, operator or event named "--flow=missing.nflow"`) {
		t.Fatalf("show after -- stderr = %q; want normal target-not-found diagnostic", stderr)
	}
	if strings.Contains(stderr, "read missing.nflow") {
		t.Fatalf("show after -- attempted to read a flow file: %q", stderr)
	}
}
