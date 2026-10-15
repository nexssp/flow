# Resilience policies

Flow’s optional `resilience` extension translates NFlow policy declarations into **Kernel** middleware. Kernel remains the sole owner of retry, timeout, concurrency, and circuit-breaker execution semantics; Flow owns option parsing and modifier-to-Kernel configuration.

## Practical quote-provider example

These examples model an application that calls an HTTP quote provider, keeps a last-known quote, and uses one breaker key for its primary and backup actions. The application host supplies `quote.primary` and `quote.backup`; each action performs the HTTP request and classifies HTTP 5xx responses as `xerr.Unavailable` and transport timeouts as `xerr.Timeout`. The example sources are in [`examples/03_resilience_quotes`](../examples/03_resilience_quotes/).

```nflow
@require resilience {
  default_max: "3"
  backoff: "exponential_jitter"
  min_backoff: "10ms"
  max_backoff: "2s"
  failure_threshold: "1"
  reset_timeout: "30s"
}

quote.primary :retry=0 :circuit_breaker=quote-provider || const @{ value: "ACME/USD 41.90 (last known)" }
```

On an HTTP 503, the host action returns a transient `Unavailable` error; the NFlow fallback returns the last-known quote using the native `const` action. The breaker still records the primary failure even though the overall expression returns the cached value. A following call using `:circuit_breaker=quote-provider` is rejected before its HTTP action runs. After the recovery window, one call is admitted as a half-open probe; a successful probe closes the breaker. Run the sources with the same configured resilience bundle instance when demonstrating shared state.

Use the native, unqualified `const` action; do not confuse it with the compile-time `@const` directive. The newly added NFlow examples use only native `const`.

The quote examples make their host boundary explicit: they are executable when the host mounts the named quote actions. The embedded self-test fixtures use deterministic in-process actions and do not make network calls. Flow’s runner integration tests execute the practical examples against local `httptest` servers and the real Kernel breaker; they cover a healthy response, a 503/cached fallback, shared-key open rejection, half-open recovery, and timeout-plus-retry composition.

## Policy behavior

- `:retry` uses `default_max`; `:retry=N` sets the action’s retry count, and `:retry=0` disables retries for that action. Kernel’s default retry predicate retries only errors classified transient by `xerr.IsTransient`. `retry_all: "true"` explicitly opts into retrying every error and is appropriate only when repeating the operation is safe.
- `:timeout=5s` bounds **each attempt**, not the full operation. Backoff and additional attempts can make total elapsed time longer than five seconds.
- `:concurrency=N` limits concurrent executions and holds the admission slot across retries.
- The breaker wraps the retry loop and observes its final outcome once. A failure increments the breaker only when `xerr.IsTransient(err)` is true. Permanent and unclassified errors are not counted as outage evidence. A successful or permanent half-open result closes the breaker; an unclassified probe reopens it without incrementing the failure count. Caller cancellation of a half-open probe also returns it to open without counting the cancellation as a service failure.
- A bare `:circuit_breaker` uses the action name as its key. `:circuit_breaker=quote-provider` shares state among actions using that key in the same registry. A shared key must use the same normalized breaker configuration.

The `@require resilience` options block accepts the retry options (`default_max`, `backoff`, `min_backoff`, `max_backoff`, `retry_all`) and breaker options (`failure_threshold`, `reset_timeout`). Unknown options and invalid values fail as typed `xerr.Validation` errors. Combining `@require resilience` with `@require retry` is a typed `xerr.Conflict`; resilience already includes retry policy options. Existing sources that only use `@require retry` remain supported.

## State lifetime and limits

Each `Bundle` instance owns an in-memory registry by default. State is process-local, volatile, and neither persisted nor shared across processes. It is not a distributed breaker. A host may inject one registry to share state across separately constructed bundles in the same process:

```go
registry := action.NewCircuitBreakerRegistry()
policy := resilience.Config{
    CircuitBreaker: action.CircuitBreakerConfig{
        FailureThreshold: 5,
        ResetTimeout:     30 * time.Second,
    },
}
bundle := resilience.BundleWithRegistry(policy, registry)
```

Share a registry only for keys whose actions are one logical dependency. Kernel does not provide an HTTP client, quote cache, idempotency guarantee, distributed coordination, persistence, or automatic action registration. The host supplies those application capabilities and must choose a correct error classification. The extension is optional and is not added to the ordinary `native.Bundles()` set; native CLI sources load it through `@require resilience`.
