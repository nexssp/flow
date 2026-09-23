package at_parallel_test

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

func TestParallel_RunsMembersConcurrently(t *testing.T) {
	src := `@parallel group {
  slow.a
  slow.b
  slow.c
}

group
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "parallel.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	var inFlight, peak atomic.Int64
	mk := func(name string) action.AnyAction {
		return action.New(name, func(_ context.Context, _ any) (string, error) {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			inFlight.Add(-1)
			return name + "-ok", nil
		}).Build()
	}

	reg := action.MustNewRegistry(action.Of(mk("slow.a"), mk("slow.b"), mk("slow.c")))

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
	if peak.Load() < 2 {
		t.Errorf("expected at least 2 concurrent members, peak=%d", peak.Load())
	}
	for _, want := range []string{"slow.a-ok", "slow.b-ok", "slow.c-ok"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestParallel_ResultKeyedByMemberName(t *testing.T) {
	src := `@parallel group {
  alpha
  beta
}

group
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "parallel.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	alpha := action.New("alpha", func(_ context.Context, _ any) (string, error) {
		return "from-alpha", nil
	}).Build()
	beta := action.New("beta", func(_ context.Context, _ any) (string, error) {
		return "from-beta", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(alpha, beta))

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
	for _, want := range []string{"from-alpha", "from-beta"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestParallel_FirstErrorFailsPipeline(t *testing.T) {
	src := `@parallel group {
  good
  bad
}

group
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "parallel.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	good := action.New("good", func(_ context.Context, _ any) (string, error) {
		return "good", nil
	}).Build()
	bad := action.New("bad", func(_ context.Context, _ any) (string, error) {
		return "", xerr.Internal("bad always fails")
	}).Build()

	reg := action.MustNewRegistry(action.Of(good, bad))

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
	if !strings.Contains(stderr.String(), "bad") && !strings.Contains(stdout.String(), "bad") {
		t.Errorf("error should name the failing member; got:\n%s\n%s",
			stdout.String(), stderr.String())
	}
}

func TestParallel_DirectiveErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			"missing_name",
			"noop\n@parallel {\n  a\n  b\n}\n",
			"missing name",
		},
		{
			"too_few_members",
			"noop\n@parallel x {\n  a\n}\n",
			"at least 2 members",
		},
		{
			"duplicate_member",
			"noop\n@parallel x {\n  a\n  a\n}\n",
			"duplicate member",
		},
		{
			"multiple_per_line",
			"noop\n@parallel x {\n  a b\n}\n",
			"one action name per line",
		},
		{
			"duplicate_declaration",
			"noop\n@parallel x {\n  a\n  b\n}\n" +
				"@parallel x {\n  c\n  d\n}\n",
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
