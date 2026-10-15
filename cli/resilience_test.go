package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/extensions/resilience"
	"github.com/nexssp/flow/runner"
)

func TestNativeRequireResilience_RetryDefaultsOverridesAndZero(t *testing.T) {
	var defaultCalls atomic.Int32
	var zeroCalls atomic.Int32
	var overrideCalls atomic.Int32
	bundle := testResilienceActions(
		testResilienceAction("test.default", func(context.Context, any) (any, error) {
			if defaultCalls.Add(1) < 3 {
				return nil, xerr.Unavailable("temporary")
			}
			return "ok", nil
		}),
		testResilienceAction("test.zero", func(context.Context, any) (any, error) {
			zeroCalls.Add(1)
			return nil, xerr.Unavailable("temporary")
		}),
		testResilienceAction("test.override", func(context.Context, any) (any, error) {
			if overrideCalls.Add(1) < 2 {
				return nil, xerr.Unavailable("temporary")
			}
			return "ok", nil
		}),
	)
	inv, err := newNativeInvocation([]compiler.Bundle{bundle}, nil)
	if err != nil {
		t.Fatal(err)
	}

	base := `@require resilience {
		default_max: "2"
		backoff: "constant"
		min_backoff: "1ns"
		max_backoff: "1ns"
	}`
	cfg := buildConfigFromSource(t, inv, base+"\ntest.default :retry", "resilience-default.nflow")
	if _, err := runner.Execute(t.Context(), cfg, base+"\ntest.default :retry", "resilience-default.nflow", nil); err != nil {
		t.Fatalf("default retry run: %v", err)
	}
	if got := defaultCalls.Load(); got != 3 {
		t.Fatalf("default retries called handler %d times, want 3", got)
	}

	zeroSource := base + "\ntest.zero :retry=0"
	if _, err := runner.Execute(t.Context(), cfg, zeroSource, "resilience-zero.nflow", nil); err == nil {
		t.Fatal("retry=0 should preserve the original action failure")
	}
	if got := zeroCalls.Load(); got != 1 {
		t.Fatalf("retry=0 called handler %d times, want 1", got)
	}

	overrideSource := base + "\ntest.override :retry=1"
	if _, err := runner.Execute(t.Context(), cfg, overrideSource, "resilience-override.nflow", nil); err != nil {
		t.Fatalf("explicit retry override run: %v", err)
	}
	if got := overrideCalls.Load(); got != 2 {
		t.Fatalf("explicit retry override called handler %d times, want 2", got)
	}

	if _, ok := cfg.Modifiers.ByName("circuit_breaker"); !ok {
		t.Fatal("native @require path did not register :circuit_breaker")
	}
	if _, ok := cfg.Modifiers.ByName("retry"); !ok {
		t.Fatal("native core :retry modifier disappeared")
	}
	count := 0
	for _, modifier := range cfg.Modifiers.All() {
		if modifier.Name == "circuit_breaker" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf(":circuit_breaker registered %d times, want 1", count)
	}
}

func TestPracticalQuoteNFlow_HealthyPrimary(t *testing.T) {
	var calls atomic.Int32
	client, endpoint := newQuoteServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "ACME/USD 42.10")
	})
	inv := newQuoteInvocation(t, client, endpoint)
	source := readQuoteExample(t, "quote_healthy.nflow")
	cfg := buildConfigFromSource(t, inv, source, "quote_healthy.nflow")

	execution, err := runner.Execute(t.Context(), cfg, source, "quote_healthy.nflow", nil)
	if err != nil {
		t.Fatalf("healthy quote flow: %v", err)
	}
	if execution.Output != "ACME/USD 42.10" || calls.Load() != 1 {
		t.Fatalf("output=%v requests=%d; want live quote and one HTTP call", execution.Output, calls.Load())
	}
}

func TestPracticalQuoteNFlow_CachedFallbackAndSharedNamedBreaker(t *testing.T) {
	var calls atomic.Int32
	client, endpoint := newQuoteServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "quote provider unavailable")
	})
	inv := newQuoteInvocation(t, client, endpoint)
	fallbackSource := readQuoteExample(t, "quote_cached_fallback.nflow")
	cfg := buildConfigFromSource(t, inv, fallbackSource, "quote_cached_fallback.nflow")

	fallback, err := runner.Execute(t.Context(), cfg, fallbackSource, "quote_cached_fallback.nflow", nil)
	if err != nil {
		t.Fatalf("cached fallback flow: %v", err)
	}
	if fallback.Output != "ACME/USD 41.90 (last known)" || calls.Load() != 1 {
		t.Fatalf("fallback output=%v requests=%d; want last-known quote and one HTTP call", fallback.Output, calls.Load())
	}

	// A different host action using the same named key is rejected by the same
	// real Kernel breaker; it never reaches the local HTTP server.
	sharedSource := readQuoteExample(t, "quote_shared_breaker.nflow")
	_, err = runner.Execute(t.Context(), cfg, sharedSource, "quote_shared_breaker.nflow", nil)
	if xerr.KindFrom(err) != xerr.KindCircuitBreaker {
		t.Fatalf("shared named-key result = %v, want CircuitBreaker", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("open shared breaker allowed another HTTP call; requests=%d", calls.Load())
	}
}

func TestPracticalQuoteNFlow_HalfOpenRecoveryProbe(t *testing.T) {
	var calls atomic.Int32
	client, endpoint := newQuoteServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "brief outage")
			return
		}
		_, _ = io.WriteString(w, "ACME/USD 42.10")
	})
	inv := newQuoteInvocation(t, client, endpoint)
	fallbackSource := withQuoteResetTimeout(t, readQuoteExample(t, "quote_cached_fallback.nflow"), "1ms")
	cfg := buildConfigFromSource(t, inv, fallbackSource, "quote_cached_fallback.nflow")
	fallback, err := runner.Execute(t.Context(), cfg, fallbackSource, "quote_cached_fallback.nflow", nil)
	if err != nil || fallback.Output != "ACME/USD 41.90 (last known)" {
		t.Fatalf("outage fallback output=%v error=%v", fallback.Output, err)
	}

	time.Sleep(10 * time.Millisecond)
	recoverySource := withQuoteResetTimeout(t, readQuoteExample(t, "quote_half_open_recovery.nflow"), "1ms")
	recovered, err := runner.Execute(t.Context(), cfg, recoverySource, "quote_half_open_recovery.nflow", nil)
	if err != nil || recovered.Output != "ACME/USD 42.10" {
		t.Fatalf("half-open recovery output=%v error=%v", recovered.Output, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("HTTP calls through failed request and recovery probe=%d, want 2", got)
	}
	if _, err := runner.Execute(t.Context(), cfg, recoverySource, "quote_half_open_recovery.nflow", nil); err != nil {
		t.Fatalf("call after successful recovery probe: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("breaker did not stay closed after probe; HTTP calls=%d, want 3", got)
	}
}

func TestPracticalQuoteNFlow_RetryTimeoutAndBreakerObserveWholeOperation(t *testing.T) {
	var calls atomic.Int32
	client, endpoint := newQuoteServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
			return
		}
		_, _ = io.WriteString(w, "ACME/USD 42.10")
	})
	inv := newQuoteInvocation(t, client, endpoint)
	source := readQuoteExample(t, "quote_retry_timeout.nflow")
	cfg := buildConfigFromSource(t, inv, source, "quote_retry_timeout.nflow")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	execution, err := runner.Execute(ctx, cfg, source, "quote_retry_timeout.nflow", nil)
	if err != nil {
		t.Fatalf("timeout then retry flow: %v", err)
	}
	if execution.Output != "ACME/USD 42.10" || calls.Load() != 2 {
		t.Fatalf("output=%v requests=%d; want successful retry after one timeout", execution.Output, calls.Load())
	}
	if _, err := runner.Execute(t.Context(), cfg, source, "quote_retry_timeout.nflow", nil); err != nil {
		t.Fatalf("breaker should remain closed after retry succeeds: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("breaker counted an intermediate retry failure; total requests=%d, want 3", calls.Load())
	}
}

func TestHostRegistrySharesStateAcrossBundleInstances(t *testing.T) {
	var failedCalls, successCalls atomic.Int32
	actions := testResilienceActions(
		testResilienceAction("test.fail", func(context.Context, any) (any, error) {
			failedCalls.Add(1)
			return nil, xerr.Unavailable("down")
		}),
		testResilienceAction("test.ok", func(context.Context, any) (any, error) {
			successCalls.Add(1)
			return "unexpected", nil
		}),
	)
	registry := action.NewCircuitBreakerRegistry()
	policy := resilience.Config{CircuitBreaker: action.CircuitBreakerConfig{FailureThreshold: 1, ResetTimeout: time.Hour}}

	first, err := newNativeInvocation([]compiler.Bundle{actions, resilience.BundleWithRegistry(policy, registry)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg1, err := first.buildConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Execute(t.Context(), cfg1, "test.fail :circuit_breaker=external-key", "host-registry-one.nflow", nil); xerr.KindFrom(err) != xerr.KindUnavailable {
		t.Fatalf("first host-registry call = %v, want Unavailable", err)
	}

	second, err := newNativeInvocation([]compiler.Bundle{actions, resilience.BundleWithRegistry(policy, registry)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg2, err := second.buildConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Execute(t.Context(), cfg2, "test.ok :circuit_breaker=external-key", "host-registry-two.nflow", nil); xerr.KindFrom(err) != xerr.KindCircuitBreaker {
		t.Fatalf("second bundle instance = %v, want shared CircuitBreaker", err)
	}
	if successCalls.Load() != 0 || failedCalls.Load() != 1 {
		t.Fatalf("handler calls fail=%d success=%d, want 1 and 0", failedCalls.Load(), successCalls.Load())
	}
}

func TestNativeRequireRetry_LegacyOptionsRemainSupported(t *testing.T) {
	var calls atomic.Int32
	bundle := testResilienceActions(testResilienceAction("test.legacy", func(context.Context, any) (any, error) {
		if calls.Add(1) == 1 {
			return nil, xerr.Unavailable("temporary")
		}
		return "ok", nil
	}))
	inv, err := newNativeInvocation([]compiler.Bundle{bundle}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := `@require retry {
		default_max: "1"
		backoff: "constant"
		min_backoff: "1ns"
		max_backoff: "1ns"
	}
test.legacy :retry`
	cfg := buildConfigFromSource(t, inv, source, "legacy-retry.nflow")
	if _, err := runner.Execute(t.Context(), cfg, source, "legacy-retry.nflow", nil); err != nil {
		t.Fatalf("legacy retry execution: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("legacy configured retry called handler %d times, want 2", got)
	}
}

func TestRequireResilienceAndRetryConflictIsExplicit(t *testing.T) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = inv.buildConfig([]require.Requirement{{Import: "resilience"}, {Import: "retry"}})
	if xerr.KindFrom(err) != xerr.KindConflict || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("conflicting bundle config error = %v, want Conflict", err)
	}
}

func buildConfigFromSource(t *testing.T, inv *invocation, source, name string) runner.Config {
	t.Helper()
	directives, err := inv.directiveTable()
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err := compiler.Preprocess(t.Context(), directives, source, name)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := inv.buildConfig(require.FromMeta(meta))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func testResilienceAction(name string, handler func(context.Context, any) (any, error)) action.AnyAction {
	return action.New[any, any](name, handler).Build()
}

func testResilienceActions(actions ...action.AnyAction) compiler.Bundle {
	return compiler.Bundle{ID: "test_actions", Libraries: []action.Library{{Name: "test", Actions: actions}}}
}

func newQuoteInvocation(t *testing.T, client *http.Client, endpoint string) *invocation {
	t.Helper()
	bundle := testResilienceActions(
		quoteHTTPAction("quote.primary", client, endpoint),
		quoteHTTPAction("quote.backup", client, endpoint),
	)
	inv, err := newNativeInvocation([]compiler.Bundle{bundle}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func quoteHTTPAction(name string, client *http.Client, endpoint string) action.AnyAction {
	return action.New[any, any](name, func(ctx context.Context, _ any) (any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
		if err != nil {
			return nil, xerr.Validation("quote request is invalid", err)
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, xerr.MapTransportError(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, xerr.MapTransportError(err)
		}
		if response.StatusCode >= http.StatusInternalServerError {
			return nil, xerr.Unavailable(fmt.Sprintf("quote endpoint returned HTTP %d", response.StatusCode))
		}
		if response.StatusCode >= http.StatusBadRequest {
			return nil, xerr.BadRequest(fmt.Sprintf("quote endpoint returned HTTP %d", response.StatusCode))
		}
		return strings.TrimSpace(string(body)), nil
	}).Build()
}

func newQuoteServer(t *testing.T, handler http.HandlerFunc) (client *http.Client, endpoint string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.Client(), server.URL
}

func readQuoteExample(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "examples", "03_resilience_quotes", name)
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read practical NFlow example %s: %v", path, err)
	}
	return string(source)
}

func withQuoteResetTimeout(t *testing.T, source, timeout string) string {
	t.Helper()
	const configured = `reset_timeout: "30s"`
	if strings.Count(source, configured) != 1 {
		t.Fatalf("expected exactly one default reset_timeout in source, got %q", source)
	}
	return strings.Replace(source, configured, fmt.Sprintf("reset_timeout: %q", timeout), 1)
}
