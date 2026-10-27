package cli

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestLintDispatchRejectsQuotedCapabilityReference(t *testing.T) {
	issues := lintOne(t, `dispatch.run @{ members: ["runtime.const"], payload: { value: "x" } }`)
	if len(issues) != 1 || issues[0].Kind != "compile" {
		t.Fatalf("lint issues = %#v, want one compile diagnostic", issues)
	}
	if !strings.Contains(issues[0].Message, "bare capability reference") {
		t.Fatalf("diagnostic = %q, want quoted capability-reference error", issues[0].Message)
	}
}

func TestLintDispatchRejectsUnknownCapabilityReference(t *testing.T) {
	issues := lintOne(t, `dispatch.run @{ members: [not.registered], payload: { value: "x" } }`)
	if len(issues) != 1 || issues[0].Kind != "compile" {
		t.Fatalf("lint issues = %#v, want one compile diagnostic", issues)
	}
	if !strings.Contains(issues[0].Message, "unknown capability") {
		t.Fatalf("diagnostic = %q, want unknown capability error", issues[0].Message)
	}
}

func TestLintCompilesPipelineMaterializer(t *testing.T) {
	issues := lintOne(t, `@pipeline local
  runtime.noop
@end`)
	if len(issues) != 0 {
		t.Fatalf("lint issues = %#v, want pipeline materializer to compile cleanly", issues)
	}
}

func TestLintValidatesPostRunAssertionSyntax(t *testing.T) {
	valid := lintOne(t, "@assert: result.total > 0\nruntime.noop")
	if len(valid) != 0 {
		t.Fatalf("lint issues for valid assertion = %#v", valid)
	}

	invalid := lintOne(t, "@assert: result.total >\nruntime.noop")
	if len(invalid) != 1 || invalid[0].Kind != "compile" {
		t.Fatalf("lint issues for invalid assertion = %#v, want one compile error", invalid)
	}
	if !strings.Contains(invalid[0].Message, "invalid expression") {
		t.Fatalf("compile diagnostic = %q, want invalid assertion expression", invalid[0].Message)
	}
}

func TestLintValidatesOnErrorConditionSyntax(t *testing.T) {
	valid := lintOne(t, "@on_error {\n  when error.kind == \"Unavailable\" -> runtime.noop\n  else -> runtime.noop\n}\nruntime.noop")
	if len(valid) != 0 {
		t.Fatalf("lint issues for valid @on_error condition = %#v", valid)
	}

	invalid := lintOne(t, "@on_error {\n  when error.kind == -> runtime.noop\n}\nruntime.noop")
	if len(invalid) != 1 || invalid[0].Kind != "compile" {
		t.Fatalf("lint issues for invalid @on_error condition = %#v, want one compile error", invalid)
	}
	if !strings.Contains(invalid[0].Message, "invalid when condition") {
		t.Fatalf("compile diagnostic = %q, want invalid condition", invalid[0].Message)
	}
}

func TestLintDoesNotInvokeWorkflowActions(t *testing.T) {
	cfg, err := buildConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	var invoked atomic.Int32
	cfg.Hooks = []action.AnyHook{{
		Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
			invoked.Add(1)
			return ctx, nil
		},
	}}

	issues := lintFile("test.nflow", `runtime.const @{ value: "compiled only" }`, cfg)
	if len(issues) != 0 {
		t.Fatalf("lint issues = %#v, want clean compile", issues)
	}
	if got := invoked.Load(); got != 0 {
		t.Fatalf("lint invoked workflow action hooks %d times", got)
	}
}
