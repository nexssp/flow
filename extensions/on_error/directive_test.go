package on_error

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runDirective(tb testing.TB, lines ...string) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	return out, err
}

func TestDirective_SingleRule(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@on_error {`,
		`  when error.kind == "Timeout" -> runtime.noop`,
		`}`,
	)
	ktest.RequireNoError(t, err)

	cfg, ok := out["on_error"].(Config)
	ktest.RequireCondition(t, ok, "on_error type = %T, want Config", out["on_error"])
	ktest.RequireEqual(t, len(cfg.Rules), 1)
	ktest.RequireEqual(t, cfg.Rules[0].Condition, `error.kind == "Timeout"`)
	ktest.RequireEqual(t, cfg.Rules[0].Target, "runtime.noop")
}

func TestDirective_MultipleRulesAndElse(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@on_error {`,
		`  when error.kind == "Timeout" -> a`,
		`  when error.kind == "NotFound" -> b`,
		`  else -> fallback`,
		`}`,
	)
	ktest.RequireNoError(t, err)

	cfg, ok := out["on_error"].(Config)
	ktest.RequireCondition(t, ok, "on_error = %T, want Config", out["on_error"])
	ktest.RequireEqual(t, len(cfg.Rules), 2)
	ktest.RequireEqual(t, cfg.ElseTarget, "fallback")
}

func TestDirective_InvalidLine(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t,
		`@on_error {`,
		`  badline`,
		`}`,
	)
	ktest.RequireErrorContains(t, err, "expected 'when")
}

func TestDirective_WhenWithoutArrow(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t,
		`@on_error {`,
		`  when error.kind == "X"`,
		`}`,
	)
	ktest.RequireErrorContains(t, err, "invalid when clause")
}

func TestResolveTarget(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Rules: []Rule{
			{Condition: `error.kind == "Timeout"`, Target: "timeout_target"},
			{Condition: `error.kind == "NotFound"`, Target: "notfound_target"},
		},
		ElseTarget: "fallback",
	}

	cases := []struct {
		name string
		kind string
		want string
	}{
		{"timeout", "Timeout", "timeout_target"},
		{"notfound", "NotFound", "notfound_target"},
		{"other falls to else", "Internal", "fallback"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]any{"error": map[string]any{"kind": c.kind, "message": "x"}}
			ktest.RequireEqual(t, resolveTarget(cfg, env), c.want)
		})
	}
}

func TestResolveTarget_NoMatchNoElse(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Rules: []Rule{{Condition: `error.kind == "Timeout"`, Target: "x"}},
	}
	env := map[string]any{"error": map[string]any{"kind": "Internal", "message": "x"}}
	ktest.RequireEqual(t, resolveTarget(cfg, env), "")
}
