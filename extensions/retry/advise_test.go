package retry

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func newBuilder() *action.Builder[any, any] {
	return action.New[any, any]("probe", func(_ context.Context, in any) (any, error) {
		return in, nil
	})
}

func TestAdvise_ConsumesRetryModifier(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 5, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	atom := &core.Atom{Name: "probe", Modifiers: []string{"timeout=1s", "retry=3", "cache=1m"}}
	builder := newBuilder()

	ktest.RequireNoError(t, advise(cfg, atom, builder))

	ktest.RequireEqual(t, atom.Modifiers, []string{"timeout=1s", "cache=1m"})
	ktest.RequireEqual(t, builder.Describe().RetryMax, 3)
}

func TestAdvise_DefaultMaxWhenNoValue(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 7, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	atom := &core.Atom{Name: "probe", Modifiers: []string{"retry"}}
	builder := newBuilder()

	ktest.RequireNoError(t, advise(cfg, atom, builder))
	ktest.RequireEqual(t, builder.Describe().RetryMax, 7)
}

func TestAdvise_RejectsBadValue(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 3, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}

	cases := []string{"retry=abc", "retry=0", "retry=-1"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			atom := &core.Atom{Name: "probe", Modifiers: []string{raw}}
			err := advise(cfg, atom, newBuilder())
			ktest.RequireCondition(t, err != nil, "expected error for %q", raw)
		})
	}
}

func TestAdvise_NoRetryModifier(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 3, MinBackoff: time.Millisecond, MaxBackoff: time.Second}
	atom := &core.Atom{Name: "probe", Modifiers: []string{"timeout=1s"}}
	builder := newBuilder()

	ktest.RequireNoError(t, advise(cfg, atom, builder))
	ktest.RequireEqual(t, atom.Modifiers, []string{"timeout=1s"})
	ktest.RequireEqual(t, builder.Describe().RetryMax, 0)
}

func TestConfigFromOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts map[string]string
		want int
	}{
		{"empty", nil, 0},
		{"valid", map[string]string{"default_max": "5"}, 5},
		{"invalid number ignored", map[string]string{"default_max": "abc"}, 0},
		{"zero ignored", map[string]string{"default_max": "0"}, 0},
		{"unknown key ignored", map[string]string{"nope": "1"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, ConfigFromOptions(c.opts).DefaultMax, c.want)
		})
	}
}

func TestConfig_Normalized(t *testing.T) {
	t.Parallel()
	var cfg Config
	got := cfg.normalized()
	ktest.RequireCondition(t, got.DefaultMax > 0, "DefaultMax not set")
	ktest.RequireCondition(t, got.MinBackoff > 0, "MinBackoff not set")
	ktest.RequireCondition(t, got.MaxBackoff >= got.MinBackoff, "MaxBackoff < MinBackoff")
}

func TestBundle_WiresAdvise(t *testing.T) {
	t.Parallel()
	b := BundleWithConfig(Config{})
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireCondition(t, b.AtomAdvise != nil, "AtomAdvise is nil")
}
