package retry

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/scope"
	"github.com/nexssp/flow/extensions/syntax"
)

func newBuilder() *action.Builder[any, any] {
	return action.New[any, any]("probe", func(_ context.Context, in any) (any, error) {
		return in, nil
	})
}

func TestAdvise_ConsumesRetryModifier(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 5, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	atom := &compiler.Atom{Name: "probe", Modifiers: []string{"timeout=1s", "retry=3", "cache=1m"}}
	builder := newBuilder()
	ktest.RequireNoError(t, advise(cfg, atom, builder))
	ktest.RequireEqual(t, atom.Modifiers, []string{"timeout=1s", "cache=1m"})
	ktest.RequireEqual(t, builder.Describe().RetryMax, 3)
}

func TestAdvise_DefaultMaxWhenNoValue(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 7, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	atom := &compiler.Atom{Name: "probe", Modifiers: []string{"retry"}}
	builder := newBuilder()
	ktest.RequireNoError(t, advise(cfg, atom, builder))
	ktest.RequireEqual(t, builder.Describe().RetryMax, 7)
}

func TestAdvise_ZeroDisablesRetryWithoutInheritedPolicy(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cfg := Config{
		DefaultMax: 3,
		Backoff:    BackoffConstant,
		MinBackoff: time.Nanosecond,
		MaxBackoff: time.Nanosecond,
	}
	atom := &compiler.Atom{Name: "probe", Modifiers: []string{"retry=0"}}
	builder := action.New[any, any]("probe", func(_ context.Context, _ any) (any, error) {
		calls.Add(1)
		return nil, xerr.Unavailable("temporary failure")
	})

	ktest.RequireNoError(t, advise(cfg, atom, builder))
	ktest.RequireEqual(t, len(atom.Modifiers), 0)
	ktest.RequireEqual(t, builder.Describe().RetryMax, 0)
	_, err := builder.Build().Do(t.Context(), nil)
	ktest.RequireCondition(t, err != nil, "expected the transient action error")
	ktest.RequireEqual(t, calls.Load(), int32(1))
}

func TestScopedZeroRetryDisablesOnlyLocalAction(t *testing.T) {
	t.Parallel()
	var disabledCalls, siblingCalls, otherScopeCalls atomic.Int32
	failingAction := func(name string, calls *atomic.Int32) action.AnyAction {
		return action.New[any, any](name, func(_ context.Context, _ any) (any, error) {
			calls.Add(1)
			return nil, xerr.Unavailable("temporary failure")
		}).Build()
	}
	resolver, err := compiler.NewDynamicResolver(action.Library{
		Name: "probe",
		Actions: []action.AnyAction{
			failingAction("probe.disabled", &disabledCalls),
			failingAction("probe.sibling", &siblingCalls),
			failingAction("probe.other_scope", &otherScopeCalls),
		},
	})
	ktest.RequireNoError(t, err)

	scopeBundle := scope.Bundle(nil)
	syntaxBundle := syntax.Bundle(nil)
	retryBundle := BundleWithConfig(Config{
		DefaultMax: 2,
		Backoff:    BackoffConstant,
		MinBackoff: time.Nanosecond,
		MaxBackoff: time.Nanosecond,
	})
	compileOptions := compiler.BundleConfig(scopeBundle, retryBundle)
	compiled, err := compiler.CompileAction(
		resolver,
		compiler.NewDirectiveTable(scopeBundle.Directives...),
		compiler.NewModifierTable(modifiers_core.Modifiers()...),
		compiler.NewOperatorTable(syntaxBundle.Operators...),
		compiler.DefaultPrimaryExtensions(),
		compileOptions...,
	).Do(t.Context(), compiler.CompileReq{
		Source: `@scope :retry=2 {
			probe.disabled:retry=0 & probe.sibling
		}
		&
		@scope :retry=1 {
			probe.other_scope
		}`,
		Name: "retry-zero-scope.nflow",
	})
	ktest.RequireNoError(t, err)

	_, err = compiled.Program.DoAny(t.Context(), nil)
	ktest.RequireCondition(t, err != nil, "expected the parallel actions to fail")
	ktest.RequireEqual(t, disabledCalls.Load(), int32(1))
	ktest.RequireEqual(t, siblingCalls.Load(), int32(3))
	ktest.RequireEqual(t, otherScopeCalls.Load(), int32(2))
}

func TestAdvise_RejectsBadValue(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 3, MinBackoff: 5 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	for _, raw := range []string{"retry=abc", "retry=-1"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			atom := &compiler.Atom{Name: "probe", Modifiers: []string{raw}}
			err := advise(cfg, atom, newBuilder())
			ktest.RequireCondition(t, err != nil, "expected error for %q", raw)
			ktest.RequireErrorContains(t, err, "retry expects a positive attempt count")
		})
	}
}

func TestAdvise_NoRetryModifier(t *testing.T) {
	t.Parallel()
	cfg := Config{DefaultMax: 3, MinBackoff: time.Millisecond, MaxBackoff: time.Second}
	atom := &compiler.Atom{Name: "probe", Modifiers: []string{"timeout=1s"}}
	builder := newBuilder()
	ktest.RequireNoError(t, advise(cfg, atom, builder))
	ktest.RequireEqual(t, atom.Modifiers, []string{"timeout=1s"})
	ktest.RequireEqual(t, builder.Describe().RetryMax, 0)
}

func TestConfigFromOptions(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigFromOptions(map[string]string{
		"default_max": "5",
		"backoff":     BackoffExponential,
		"min_backoff": "25ms",
		"max_backoff": "750ms",
		"retry_all":   "true",
	})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg.DefaultMax, 5)
	ktest.RequireEqual(t, cfg.Backoff, BackoffExponential)
	ktest.RequireEqual(t, cfg.MinBackoff, 25*time.Millisecond)
	ktest.RequireEqual(t, cfg.MaxBackoff, 750*time.Millisecond)
	ktest.RequireEqual(t, cfg.RetryAll, true)
}

func TestConfigFromOptions_Defaults(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigFromOptions(nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, cfg.DefaultMax, 3)
	ktest.RequireEqual(t, cfg.Backoff, BackoffExponentialJitter)
	ktest.RequireEqual(t, cfg.MinBackoff, 10*time.Millisecond)
	ktest.RequireEqual(t, cfg.MaxBackoff, 2*time.Second)
	ktest.RequireEqual(t, cfg.RetryAll, false)
}

func TestConfigFromOptions_RejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts map[string]string
	}{
		{name: "bad count", opts: map[string]string{"default_max": "abc"}},
		{name: "zero count", opts: map[string]string{"default_max": "0"}},
		{name: "negative count", opts: map[string]string{"default_max": "-1"}},
		{name: "unknown backoff", opts: map[string]string{"backoff": "magic"}},
		{name: "bad duration", opts: map[string]string{"min_backoff": "soon"}},
		{name: "zero duration", opts: map[string]string{"max_backoff": "0s"}},
		{name: "invalid bool", opts: map[string]string{"retry_all": "yes"}},
		{name: "max below min", opts: map[string]string{"min_backoff": "2s", "max_backoff": "1s"}},
		{name: "unknown option", opts: map[string]string{"nope": "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ConfigFromOptions(tc.opts)
			ktest.RequireCondition(t, err != nil, "expected invalid retry configuration to fail")
		})
	}
}

func TestConfigBackoffStrategies(t *testing.T) {
	t.Parallel()
	const base = 100 * time.Millisecond
	constant := Config{Backoff: BackoffConstant, MinBackoff: base}.backoffFunc()
	ktest.RequireEqual(t, constant(3), base)

	exponential := Config{Backoff: BackoffExponential, MinBackoff: base, MaxBackoff: time.Second}.backoffFunc()
	ktest.RequireEqual(t, exponential(2), 2*base)

	jitter := Config{Backoff: BackoffExponentialJitter, MinBackoff: base, MaxBackoff: time.Second}.backoffFunc()
	got := jitter(1)
	ktest.RequireCondition(t, got >= 70*time.Millisecond && got <= 130*time.Millisecond, "jitter delay %s outside expected range", got)
}

func TestAdvise_RetryAllRetriesPermanentErrors(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	builder := action.New[any, any]("probe", func(_ context.Context, _ any) (any, error) {
		calls.Add(1)
		return nil, xerr.BadRequest("permanent failure")
	})
	cfg := Config{
		DefaultMax: 2,
		Backoff:    BackoffConstant,
		MinBackoff: time.Nanosecond,
		MaxBackoff: time.Nanosecond,
		RetryAll:   true,
	}
	atom := &compiler.Atom{Name: "probe", Modifiers: []string{"retry=2"}}
	ktest.RequireNoError(t, advise(cfg, atom, builder))
	_, err := builder.Build().Do(context.Background(), nil)
	ktest.RequireCondition(t, err != nil, "expected the permanent action error")
	ktest.RequireEqual(t, calls.Load(), int32(3))
}

func TestBundle_RejectsInvalidOptionDuringAdvice(t *testing.T) {
	t.Parallel()
	bundle := Bundle(map[string]string{"retry_all": "sometimes"})
	err := bundle.AtomAdvise(&compiler.Atom{Name: "probe"}, newBuilder())
	ktest.RequireCondition(t, err != nil, "expected invalid bundle option error")
	ktest.RequireCondition(t, strings.Contains(err.Error(), "retry_all"), "error %q should name retry_all", err)
}

func TestBundleWithConfig_RejectsInvalidDirectConfig(t *testing.T) {
	t.Parallel()
	bundle := BundleWithConfig(Config{Backoff: "unknown"})
	err := bundle.AtomAdvise(&compiler.Atom{Name: "probe"}, newBuilder())
	ktest.RequireCondition(t, err != nil, "expected invalid direct config error")
}

func TestBundle_WiresAdvise(t *testing.T) {
	t.Parallel()
	bundle := BundleWithConfig(Config{})
	ktest.RequireEqual(t, bundle.ID, ID)
	ktest.RequireEqual(t, len(bundle.Libraries), 1)
	ktest.RequireCondition(t, bundle.AtomAdvise != nil, "AtomAdvise is nil")
	ktest.RequireEqual(t, bundle.AcceptedOptions, acceptedOptions)
}
