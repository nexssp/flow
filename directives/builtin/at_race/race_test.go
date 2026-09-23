package at_race_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/flow"
	flowrunner "github.com/nexssp/flow/runner"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

func TestRace_ReturnsFirstSuccess(t *testing.T) {
	src := `@race group {
  fast
  slow
}

group
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "race.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	fast := action.New("fast", func(_ context.Context, _ any) (string, error) {
		return "fast-won", nil
	}).Build()
	slow := action.New("slow", func(ctx context.Context, _ any) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return "slow-won", nil
		}
	}).Build()

	reg := action.MustNewRegistry(action.Of(fast, slow))

	var stdout, stderr strings.Builder
	exit := flowrunner.RunWithRegistry(context.Background(), flowrunner.Request{
		Path:    flowPath,
		Payload: map[string]any{},
		Args:    []string{"--approval=none"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}, reg, nil)

	if exit != 0 {
		t.Fatalf("exit=%d\nstdout:\n%s\nstderr:\n%s",
			exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "fast-won") {
		t.Errorf("expected 'fast-won' in output; got:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "slow-won") {
		t.Errorf("slow should not have won; got:\n%s", stdout.String())
	}
}

func TestRace_LoserCanceledViaContext(t *testing.T) {
	src := `@race group {
  fast
  slow
}

group
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "race.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	fast := action.New("fast", func(_ context.Context, _ any) (string, error) {
		return "fast-won", nil
	}).Build()

	var slowSawCancel atomic.Bool
	slow := action.New("slow", func(ctx context.Context, _ any) (string, error) {
		select {
		case <-ctx.Done():
			slowSawCancel.Store(true)
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
			return "slow-won", nil
		}
	}).Build()

	reg := action.MustNewRegistry(action.Of(fast, slow))

	var stdout, stderr strings.Builder
	exit := flowrunner.RunWithRegistry(context.Background(), flowrunner.Request{
		Path:    flowPath,
		Payload: map[string]any{},
		Args:    []string{"--approval=none"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}, reg, nil)

	if exit != 0 {
		t.Fatalf("exit=%d\nstdout:\n%s\nstderr:\n%s",
			exit, stdout.String(), stderr.String())
	}

	// Give the loser a moment to observe cancellation.
	time.Sleep(50 * time.Millisecond)
	if !slowSawCancel.Load() {
		t.Error("expected slow to observe context cancellation")
	}
}

func TestRace_AllFailReturnsLastError(t *testing.T) {
	src := `@race group {
  a
  b
}

group
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "race.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	a := action.New("a", func(_ context.Context, _ any) (string, error) {
		return "", xerr.Timeout("a timed out")
	}).Build()
	b := action.New("b", func(_ context.Context, _ any) (string, error) {
		return "", xerr.Unavailable("b unavailable")
	}).Build()

	reg := action.MustNewRegistry(action.Of(a, b))

	var stdout, stderr strings.Builder
	exit := flowrunner.RunWithRegistry(context.Background(), flowrunner.Request{
		Path:    flowPath,
		Payload: map[string]any{},
		Args:    []string{"--approval=none"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}, reg, nil)

	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "timed out") && !strings.Contains(combined, "unavailable") {
		t.Errorf("expected one of the member errors; got:\n%s", combined)
	}
}

func TestRace_DirectiveErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			"missing_name",
			"noop\n@race {\n  a\n  b\n}\n",
			"missing name",
		},
		{
			"too_few_members",
			"noop\n@race x {\n  a\n}\n",
			"at least 2 members",
		},
		{
			"duplicate_member",
			"noop\n@race x {\n  a\n  a\n}\n",
			"duplicate member",
		},
		{
			"multiple_per_line",
			"noop\n@race x {\n  a b\n}\n",
			"one action name per line",
		},
		{
			"duplicate_declaration",
			"noop\n@race x {\n  a\n  b\n}\n" +
				"@race x {\n  c\n  d\n}\n",
			"duplicate declaration",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "bad.nflow")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := flow.Preprocess(path)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}
