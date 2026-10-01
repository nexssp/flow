package hook

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runDirective(tb testing.TB, line string) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: []string{line},
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	return out, err
}

func TestDirective_SingleHook(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@hook:hook.verify`)
	ktest.RequireNoError(t, err)

	hooks, _ := out["hooks"].([]string)
	ktest.RequireEqual(t, hooks, []string{"hook.verify"})
}

func TestDirective_MultipleHooks(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@hook:hook.verify,observe.metrics`)
	ktest.RequireNoError(t, err)

	hooks, _ := out["hooks"].([]string)
	ktest.RequireEqual(t, hooks, []string{"hook.verify", "observe.metrics"})
}

func TestDirective_NoColonForm(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@hook hook.verify`)
	ktest.RequireNoError(t, err)

	hooks, _ := out["hooks"].([]string)
	ktest.RequireEqual(t, hooks, []string{"hook.verify"})
}

func TestDirective_EmptyRejected(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@hook:`)
	ktest.RequireErrorContains(t, err, "at least one hook")
}

func TestDirective_Accumulates(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	for _, line := range []string{`@hook:a`, `@hook:b,c`} {
		_, err := handleDirective(context.Background(), core.DirectiveReq{
			Lines: []string{line},
			I:     0,
			Out:   out,
			File:  "<test>",
		})
		ktest.RequireNoError(t, err)
	}
	hooks, _ := out["hooks"].([]string)
	ktest.RequireEqual(t, hooks, []string{"a", "b", "c"})
}
