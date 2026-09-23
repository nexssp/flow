package at_fallback_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow"
	flowrunner "github.com/nexssp/flow/runner"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

func TestFallback_RoutesOnErrorKind(t *testing.T) {
	src := `@fallback primary {
  when error.kind == "Timeout" -> backup.timed
  else -> backup.generic
}

primary
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "fallback.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	primary := action.New("primary", func(_ context.Context, _ map[string]any) (string, error) {
		return "", xerr.Timeout("primary timed out")
	}).Build()
	backupTimed := action.New("backup.timed", func(_ context.Context, _ any) (string, error) {
		return "recovered:timed", nil
	}).Build()
	backupGeneric := action.New("backup.generic", func(_ context.Context, _ any) (string, error) {
		return "recovered:generic", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(primary, backupTimed, backupGeneric))

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
	if !strings.Contains(stdout.String(), "recovered:timed") {
		t.Errorf("expected 'recovered:timed' in output; got:\n%s", stdout.String())
	}
}

func TestFallback_OnlyAppliesToNamedAtom(t *testing.T) {
	src := `@fallback first {
  else -> backup
}

first -> second
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "fallback.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	first := action.New("first", func(_ context.Context, in any) (any, error) {
		return in, nil
	}).Build()
	second := action.New("second", func(_ context.Context, _ any) (string, error) {
		return "", xerr.Internal("second always fails")
	}).Build()
	backup := action.New("backup", func(_ context.Context, _ any) (string, error) {
		return "backup ran", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(first, second, backup))

	var stdout, stderr strings.Builder
	exit := flowrunner.RunWithRegistry(context.Background(), flowrunner.Request{
		Path:    flowPath,
		Payload: map[string]any{},
		Args:    []string{"--approval=none"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}, reg, nil)

	// second has no fallback, so the pipeline fails.
	if exit == 0 {
		t.Fatal("expected non-zero exit — second has no fallback")
	}
	if strings.Contains(stdout.String(), "backup ran") {
		t.Errorf("backup should not have run; got:\n%s", stdout.String())
	}
}

func TestFallback_WrapsRetryFailures(t *testing.T) {
	src := `@retry flaky {
  attempts: 2
  backoff: constant
  base: 1ms
  only: [Timeout]
}

@fallback flaky {
  when error.kind == "Timeout" -> rescue
}

flaky
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "fallback.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	flaky := action.New("flaky", func(_ context.Context, _ map[string]any) (string, error) {
		calls.Add(1)
		return "", xerr.Timeout("always times out")
	}).Build()
	rescue := action.New("rescue", func(_ context.Context, _ any) (string, error) {
		return "rescued", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(flaky, rescue))

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
	// 1 initial + 2 retries = 3 calls before fallback fires.
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 calls before fallback, got %d", got)
	}
	if !strings.Contains(stdout.String(), "rescued") {
		t.Errorf("expected 'rescued' in output; got:\n%s", stdout.String())
	}
}

func TestFallback_DirectiveErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			"missing_name",
			"noop\n@fallback {\n  else -> x\n}\n",
			"missing name",
		},
		{
			"empty_block",
			"noop\n@fallback x {\n}\n",
			"needs at least one",
		},
		{
			"unknown_kind",
			"noop\n@fallback x {\n  when error.kind == \"Bogus\" -> y\n}\n",
			"unknown xerr kind",
		},
		{
			"duplicate",
			"noop\n@fallback x {\n  else -> y\n}\n" +
				"@fallback x {\n  else -> z\n}\n",
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
