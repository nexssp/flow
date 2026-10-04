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
