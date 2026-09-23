package at_on_error_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/contracts"
	flowrunner "github.com/nexssp/flow/runner"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

func TestOnError_RoutesByKind(t *testing.T) {
	src := `failing.action
@on_error {
  when error.kind == "Timeout" -> recovery.timeout
  when error.kind == "Validation" -> recovery.validation
  else -> recovery.generic
}
`
	cases := []struct {
		name      string
		failKind  xerr.Kind
		wantLabel string
	}{
		{"timeout routes to timeout recovery", xerr.KindTimeout, "recovered:timeout"},
		{"validation routes to validation recovery", xerr.KindValidation, "recovered:validation"},
		{"unavailable falls to else", xerr.KindUnavailable, "recovered:generic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			flowPath := filepath.Join(dir, "on_error.nflow")
			if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}

			failing := action.New("failing.action", func(_ context.Context, _ map[string]any) (any, error) {
				return nil, makeAppError(tc.failKind)
			}).Build()
			recoveryTimeout := action.New("recovery.timeout", func(_ context.Context, _ any) (any, error) {
				return "recovered:timeout", nil
			}).Build()
			recoveryValidation := action.New("recovery.validation", func(_ context.Context, _ any) (any, error) {
				return "recovered:validation", nil
			}).Build()
			recoveryGeneric := action.New("recovery.generic", func(_ context.Context, _ any) (any, error) {
				return "recovered:generic", nil
			}).Build()

			reg := action.MustNewRegistry(action.Of(
				failing, recoveryTimeout, recoveryValidation, recoveryGeneric,
			))

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
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, tc.wantLabel) {
				t.Errorf("expected %q in output; got:\n%s", tc.wantLabel, combined)
			}
		})
	}
}

func TestOnError_RecoveredErrorReachesTarget(t *testing.T) {
	src := `failing.action
@on_error {
  else -> recovery.inspect
}
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "on_error.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	failing := action.New("failing.action", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, xerr.Forbidden("policy denial")
	}).Build()

	var seen *contracts.RecoveredError
	inspector := action.New("recovery.inspect", func(ctx context.Context, _ any) (any, error) {
		r, ok := contracts.RecoveredErrorFrom(ctx)
		if !ok {
			return nil, xerr.Internal("recovered error missing from context")
		}
		seen = r
		return "inspected", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(failing, inspector))

	var stdout, stderr strings.Builder
	exit := flowrunner.RunWithRegistry(context.Background(), flowrunner.Request{
		Path:    flowPath,
		Payload: map[string]any{},
		Args:    []string{"--approval=none"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}, reg, nil)

	if exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", exit, stdout.String(), stderr.String())
	}
	if seen == nil {
		t.Fatal("recovery target did not see the recovered error")
	}
	if seen.Kind != "Forbidden" {
		t.Errorf("Kind = %q, want Forbidden", seen.Kind)
	}
}

func TestOnError_NoMatchNoElse_PropagatesOriginalError(t *testing.T) {
	src := `failing.action
@on_error {
  when error.kind == "Timeout" -> recovery.unused
}
`
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "on_error.nflow")
	if err := os.WriteFile(flowPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	failing := action.New("failing.action", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, xerr.Validation("bad input")
	}).Build()
	unused := action.New("recovery.unused", func(_ context.Context, _ any) (any, error) {
		return "should not run", nil
	}).Build()

	reg := action.MustNewRegistry(action.Of(failing, unused))

	var stdout, stderr strings.Builder
	exit := flowrunner.RunWithRegistry(context.Background(), flowrunner.Request{
		Path:    flowPath,
		Payload: map[string]any{},
		Args:    []string{"--approval=none"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}, reg, nil)

	if exit == 0 {
		t.Fatal("expected non-zero exit when nothing matches")
	}
	if !strings.Contains(stdout.String(), "bad input") {
		t.Errorf("original error should reach stdout; got:\n%s", stdout.String())
	}
}

func TestOnError_DirectiveErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			"no_name_allowed",
			"noop\n@on_error foo {\n  when error.suspended -> x\n}\n",
			"takes no name",
		},
		{
			"empty_block",
			"noop\n@on_error {\n}\n",
			"at least one",
		},
		{
			"unknown_kind",
			"noop\n@on_error {\n  when error.kind == \"Bogus\" -> x\n}\n",
			"unknown xerr kind",
		},
		{
			"unsupported_condition",
			"noop\n@on_error {\n  when error.code == \"ai.x\" -> x\n}\n",
			"unsupported condition",
		},
		{
			"duplicate",
			"noop\n@on_error {\n  when error.suspended -> x\n}\n" +
				"@on_error {\n  when error.suspended -> y\n}\n",
			"duplicate",
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

func makeAppError(kind xerr.Kind) error {
	switch kind {
	case xerr.KindTimeout:
		return xerr.Timeout("simulated timeout")
	case xerr.KindValidation:
		return xerr.Validation("simulated validation failure")
	case xerr.KindUnavailable:
		return xerr.Unavailable("simulated unavailability")
	default:
		return xerr.Internal("simulated internal failure")
	}
}
