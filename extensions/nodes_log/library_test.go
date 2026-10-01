package nodes_log

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

// captureLogs swaps slog.Default for a text handler writing into a
// buffer, then restores it. Used to assert emission without touching
// os.Stdout.
func captureLogs(tb testing.TB, fn func()) string {
	tb.Helper()
	var buffer bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buffer, nil)))
	defer slog.SetDefault(original)
	fn()
	return buffer.String()
}

func TestLog_EmitsAndPassesThrough(t *testing.T) {
	cases := []struct {
		name  string
		level string
		act   action.AnyAction
	}{
		{"info", "INFO", LogInfo},
		{"warn", "WARN", LogWarn},
		{"error", "ERROR", LogError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := map[string]any{"value": c.name, "message": "test line"}

			var output any
			logged := captureLogs(t, func() {
				output, _ = action.InvokeAny(context.Background(), c.act, input)
			})

			result, ok := output.(map[string]any)
			ktest.RequireCondition(t, ok, "output type = %T, want map[string]any", output)
			ktest.RequireEqual(t, result, input)

			ktest.RequireCondition(t, strings.Contains(logged, c.level),
				"log output missing level %q: %s", c.level, logged)
			ktest.RequireStringContains(t, logged, "test line")
			ktest.RequireStringContains(t, logged, "value="+c.name)
		})
	}
}

func TestBundle_WiresActionsAndFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}

func TestLibrary_ActionNames(t *testing.T) {
	t.Parallel()
	want := map[string]bool{"log.info": false, "log.warn": false, "log.error": false}
	for _, a := range Library().Actions {
		if _, ok := want[a.Describe().Name]; ok {
			want[a.Describe().Name] = true
		}
	}
	for name, seen := range want {
		ktest.RequireCondition(t, seen, "action %q missing", name)
	}
}
