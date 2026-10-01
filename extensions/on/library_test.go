package on

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runDirective(tb testing.TB, line string) map[string]any {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: []string{line},
		I:     0,
		Out:   out,
	})
	ktest.RequireNoError(tb, err)
	return out
}

func TestDirective_EventTrigger(t *testing.T) {
	t.Parallel()
	out := runDirective(t, `@on event "nats:orders.incoming"`)

	trigger, ok := out["on_event"].(EventTrigger)
	ktest.RequireCondition(t, ok, "on_event type = %T, want EventTrigger", out["on_event"])
	ktest.RequireEqual(t, trigger.Protocol, "nats")
	ktest.RequireEqual(t, trigger.Target, "orders.incoming")
}

func TestDirective_HTTPTrigger(t *testing.T) {
	t.Parallel()
	out := runDirective(t, `@on event "http::8080"`)

	trigger, ok := out["on_event"].(EventTrigger)
	ktest.RequireCondition(t, ok, "on_event = %T, want EventTrigger", out["on_event"])
	ktest.RequireEqual(t, trigger.Protocol, "http")
	ktest.RequireEqual(t, trigger.Target, ":8080")
}

func TestDirective_NoEventKeyword(t *testing.T) {
	t.Parallel()
	out := runDirective(t, `@on start "nats:x"`)
	_, present := out["on_event"]
	ktest.RequireCondition(t, !present, "on_event should not be set when keyword is not 'event'")
}

func TestDirective_NoColon(t *testing.T) {
	t.Parallel()
	out := runDirective(t, `@on event "noColonHere"`)
	_, present := out["on_event"]
	ktest.RequireCondition(t, !present, "on_event should not be set without a colon")
}

func TestBundle_WiresDirective(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "on")
}
