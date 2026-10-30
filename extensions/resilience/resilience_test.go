package resilience

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/retry"
)

func TestConfigFromOptions_DefaultsAndOverrides(t *testing.T) {
	t.Parallel()
	defaults, err := ConfigFromOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Retry.DefaultMax != 3 || defaults.Retry.Backoff != retry.BackoffExponentialJitter ||
		defaults.Retry.MinBackoff != 10*time.Millisecond || defaults.Retry.MaxBackoff != 2*time.Second {
		t.Fatalf("retry defaults = %+v", defaults.Retry)
	}
	if defaults.CircuitBreaker.FailureThreshold != 5 || defaults.CircuitBreaker.ResetTimeout != 30*time.Second {
		t.Fatalf("breaker defaults = %+v", defaults.CircuitBreaker)
	}

	configured, err := ConfigFromOptions(map[string]string{
		"default_max":       "7",
		"backoff":           "constant",
		"min_backoff":       "2ms",
		"max_backoff":       "20ms",
		"failure_threshold": "2",
		"reset_timeout":     "250ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.Retry.DefaultMax != 7 || configured.Retry.Backoff != retry.BackoffConstant ||
		configured.Retry.MinBackoff != 2*time.Millisecond || configured.Retry.MaxBackoff != 20*time.Millisecond {
		t.Fatalf("explicit retry settings = %+v", configured.Retry)
	}
	if configured.CircuitBreaker.FailureThreshold != 2 || configured.CircuitBreaker.ResetTimeout != 250*time.Millisecond {
		t.Fatalf("explicit breaker settings = %+v", configured.CircuitBreaker)
	}
}

func TestConfigFromOptions_RejectsBadOptions(t *testing.T) {
	t.Parallel()
	for _, options := range []map[string]string{
		{"failure_threshold": "0"},
		{"reset_timeout": "soon"},
		{"retry_all": "sometimes"},
		{"retry_predicate": "true"},
		{"unknown": "value"},
	} {
		if _, err := ConfigFromOptions(options); xerr.KindFrom(err) != xerr.KindValidation {
			t.Fatalf("options %v error = %v, want Validation", options, err)
		}
	}
}

func TestAdviseConsumesAllPoliciesAndPreservesOtherModifiers(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigFromOptions(map[string]string{
		"default_max":       "3",
		"backoff":           "constant",
		"min_backoff":       "1ns",
		"max_backoff":       "1ns",
		"failure_threshold": "2",
		"reset_timeout":     "1s",
	})
	if err != nil {
		t.Fatal(err)
	}
	atom := &compiler.Atom{Name: "test.action", Modifiers: []string{
		"retry=1", "timeout=20ms", "concurrency=2", "circuit_breaker=shared", "cache=1m",
	}}
	builder := action.New[any, any]("test.action", func(context.Context, any) (any, error) { return nil, nil })
	if err := advise(cfg, atom, builder); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(atom.Modifiers, ","); got != "cache=1m" {
		t.Fatalf("remaining modifiers = %q, want cache=1m", got)
	}
	if builder.Describe().RetryMax != 1 {
		t.Fatalf("metadata retry max = %d, want 1", builder.Describe().RetryMax)
	}
}

func TestAdviseConsumesTypedTimeoutAndPreservesOtherModifierValues(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigFromOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	timeout := compiler.ModifierValue{Raw: "timeout=policy.short", Value: 5 * time.Second, Typed: true}
	cache := compiler.ModifierValue{Raw: "cache=1m"}
	atom := &compiler.Atom{
		Name:           "test.typed_timeout",
		Modifiers:      []string{timeout.Raw, cache.Raw},
		ModifierValues: []compiler.ModifierValue{timeout, cache},
	}
	builder := action.New[any, any]("test.typed_timeout", func(context.Context, any) (any, error) { return nil, nil })
	if err := advise(cfg, atom, builder); err != nil {
		t.Fatal(err)
	}
	if got := builder.Describe().Timeout; got != 5*time.Second {
		t.Fatalf("metadata timeout = %s, want 5s", got)
	}
	if got := strings.Join(atom.Modifiers, ","); got != "cache=1m" {
		t.Fatalf("remaining modifiers = %q, want cache=1m", got)
	}
	if len(atom.ModifierValues) != 1 || atom.ModifierValues[0].Raw != "cache=1m" {
		t.Fatalf("remaining modifier values = %#v, want only cache=1m", atom.ModifierValues)
	}
}

func TestAdviseRetryZeroDisablesRetry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cfg, err := ConfigFromOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	atom := &compiler.Atom{Name: "test.zero", Modifiers: []string{"retry=0"}}
	builder := action.New[any, any]("test.zero", func(context.Context, any) (any, error) {
		calls.Add(1)
		return nil, xerr.Unavailable("temporary")
	})
	if err := advise(cfg, atom, builder); err != nil {
		t.Fatal(err)
	}
	if len(atom.Modifiers) != 0 {
		t.Fatalf("retry=0 modifier was not consumed: %v", atom.Modifiers)
	}
	_, err = builder.Build().Do(t.Context(), nil)
	if err == nil || calls.Load() != 1 {
		t.Fatalf("retry=0 error=%v calls=%d; want one attempt", err, calls.Load())
	}
}

func TestBundleRegistrationHasOnlyCircuitBreakerModifier(t *testing.T) {
	t.Parallel()
	bundle := Bundle(nil)
	if bundle.ID != ID {
		t.Fatalf("bundle ID = %q", bundle.ID)
	}
	if len(bundle.Modifiers) != 1 || bundle.Modifiers[0].Name != "circuit_breaker" || !bundle.Modifiers[0].Unique || !bundle.Modifiers[0].Inheritable {
		t.Fatalf("resilience modifiers = %+v", bundle.Modifiers)
	}
	if _, ok := compiler.Lookup(ID); !ok {
		t.Fatal("resilience @require factory is not registered")
	}
}

func TestBundleRejectsInvalidDirectConfigWithoutPanicking(t *testing.T) {
	t.Parallel()
	bundle := BundleWithConfig(Config{CircuitBreaker: action.CircuitBreakerConfig{FailureThreshold: -1}})
	atom := &compiler.Atom{Name: "test.invalid", Modifiers: []string{"circuit_breaker"}}
	builder := action.New[any, any]("test.invalid", func(context.Context, any) (any, error) { return nil, nil })
	if err := bundle.AtomAdvise(atom, builder); xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("invalid direct config error = %v, want Validation", err)
	}
}

func TestAdviseInvalidModifiersUseValidationKind(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigFromOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, modifiers := range [][]string{
		{"retry=-1"},
		{"timeout=soon"},
		{"concurrency=0"},
		{"timeout=1s", "timeout=2s"},
	} {
		atom := &compiler.Atom{Name: "test.invalid", Modifiers: modifiers}
		builder := action.New[any, any]("test.invalid", func(context.Context, any) (any, error) { return nil, nil })
		if err := advise(cfg, atom, builder); xerr.KindFrom(err) != xerr.KindValidation {
			t.Fatalf("modifiers %v error = %v, want Validation", modifiers, err)
		}
	}
}
