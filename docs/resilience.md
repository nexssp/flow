# Build resilient workflows with profiles

## What NFlow cannot express yet

Kernel contains resilience features that Flow does not currently expose as NFlow policy syntax. A profile cannot switch these on. They require a Go host integration or a future DSL/API:

- Adaptive or smart resilience policies, adaptive load shedding, and priority-based shedding.
- External admission permits; distributed rate limiters; shared locks, leader-only execution, and renewable fenced leases.
- Delayed hedging, generic graceful-drain supervision, and Saga orchestration.
- Custom retry-error predicates, linear backoff, and `Builder.Once()`.
- Explicit resume-identity validation and custom cache-layer selection.

Flow also does not ship Redis/database implementations for shared journals, limiters, caches, or locks. A local modifier or a file journal does not become cluster-wide just because several workers use the same workflow.

## What `@require resilience` actually does

`@require resilience` loads Flow’s optional resilience bundle. It connects Flow’s retry and circuit-breaker modifiers to Kernel policies and lets you set bundle defaults, such as retry/backoff behavior and breaker thresholds. It does **not** define a workflow profile, automatically wrap every action in a flow, or share policy state across processes.

Profiles are a separate, reusable Flow feature: `@profile` names a set of modifiers, and `:profile=NAME` applies that set to an `@pipeline`. Define profiles before the pipeline that uses them. Profiles can inherit from a parent, and a child or pipeline-local modifier overrides the inherited modifier with the same name.

```nflow
@require resilience

@profile provider_read
  :timeout=250ms
  :retry=2
  :circuit_breaker=pricing-provider
  :concurrency=8
  :rate_limit=20
  :cache=5s

@profile replay_safe_write
  :timeout=1s
  :idempotent

@profile payment_authorization
  :parent=replay_safe_write
  :timeout=2s
  :retry=2

@pipeline fetch_quote :profile=provider_read
  pricing.fetch
@end
```

A profile belongs on a pipeline boundary, not directly on an arbitrary action. Put a provider call in its own small pipeline and apply the profile there; then compose that stage with validation, mapping, and other stages. This makes the policy boundary visible and avoids accidentally retrying an entire business workflow, including side effects. `:profile=...` is expanded on pipeline headers; do not assume it works as `@scope :profile=...` or on an ordinary action line. Use explicit modifiers on an action or a dedicated profiled pipeline instead.

To see the effective policy on each atom, run `nflow explain <file.nflow>`; `nflow lint <file.nflow>` checks that the profiles and modifiers resolve before execution.

## Try complete workflows

The examples below model an order checkout rather than just a single failing call. They are complete, deterministic fixtures: the built-in `const`, `fail`, and `sleep` keywords stand in for pricing, inventory, payment, and provider actions so you can run the control flow without credentials or a network service. In an application, replace those adapter stubs with your mounted service actions and keep the profile and recovery boundaries.

`const` is a source-level keyword for the canonical `runtime.const` action, so examples need not spell out the `runtime.` prefix. Direct-object mode emits only the fields declared in `@{ ... }`; a bare field such as `quantity` copies that value from the current input. Use `const @{ value: ... }` when the result is a scalar or array, or when an object itself needs a top-level `value` or `val` field.

| Workflow | What it demonstrates | Expected outcome |
|---|---|---|
| [checkout_profile.nflow](../extensions/resilience/nflows/checkout_profile.nflow) | Validates an order, fetches a quote, reserves inventory, and authorizes payment through separate profiled stages. The payment profile inherits the local idempotency/timeout base and adds bounded retry. | Accepted order with a calculated total and an order ID available as the real provider-key basis. |
| [checkout_degraded.nflow](../extensions/resilience/nflows/checkout_degraded.nflow) | Retries a simulated pricing-provider outage under a read profile, routes only `Unavailable`/`Timeout` into a last-known quote, and holds the order safely. | A stale estimate is shown, but inventory is not reserved and payment is not started. |

Run either flow directly, inspect its effective profile, and run the entire resilience fixture set:

```sh
nflow run extensions/resilience/nflows/checkout_profile.nflow
nflow run extensions/resilience/nflows/checkout_degraded.nflow
nflow explain extensions/resilience/nflows/checkout_profile.nflow
nflow test ./extensions/resilience
nflow self test resilience
```

Install guidance is intentionally version-generic; from a Flow checkout, the current CLI can be installed with:

```sh
go install github.com/nexssp/flow/cmd/nflow@latest
```

The key safety choice in the degraded workflow is business-specific: a last-known price is useful as an **estimate**, not as permission to charge the customer. The flow requires confirmation and does not call the inventory or payment stages. In the successful fixture, `:idempotent` demonstrates process-local duplicate protection; a real payment or inventory adapter must also send a stable key (for example, the order ID plus operation name) to a provider/database that enforces idempotency. A local modifier alone cannot protect a write across restarts or replicas.

The focused [`quote_*` fixtures](../extensions/resilience/nflows/) remain useful for isolating retry, timeout, breaker, typed-recovery, and journal mechanics. They are mechanism tests; the two checkout flows are the end-to-end patterns to adapt.

## Measure local runtime cost

The integrated [resilience benchmark](../spec/04_resilience_benchmark/) compares a deterministic no-I/O workflow with the same workload under a timeout, retry, circuit breaker, and concurrency limit. Run it from the repository root:

```sh
go test ./spec/04_resilience_benchmark -run '^$' -bench '^BenchmarkResilienceWorkflow$' -benchmem -count=3
```

The `compiled_runtime` results measure repeated execution after one compile; `compile_and_run` includes preprocessing and compilation each time. Go reports `ns/op`, `B/op`, and `allocs/op`. These are local synthetic measurements, not CLI startup, network or database latency, production capacity, or a service-level guarantee. Compare the variants on the same machine and toolchain; `nflow test ./spec` checks fixture correctness but does not benchmark them.

For immediate per-pattern measurements through the CLI, list and run the embedded NFlow suite:

```sh
nflow self bench resilience --list
nflow self bench resilience --iterations 50 --warmup 3
```

The retry case exercises one retry with a 1ms constant backoff and `retry_all`; the breaker trips once, so warm samples exercise its open path. Timeout and rate-limit samples stay on successful, non-throttled fast paths. Concurrency, deduplication, and coalescing run sequentially, so they measure middleware overhead—not contention or shared-work savings. Cache and idempotency warm samples are hits. These are local Flow-runner timings; they are not measurements of network backends, CLI startup, or production capacity.

As a concrete reference, one three-sample run on Linux/amd64 (Intel Xeon at 2.50 GHz) produced these medians:

| Phase | Workflow | Median time/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Compile + run | Baseline | 324.6 µs | 182,599 | 1,048 |
| Compile + run | Resilience profile | 394.5 µs | 195,893 | 1,198 |
| Precompiled runtime | Baseline | 12.7 µs | 3,408 | 43 |
| Precompiled runtime | Resilience profile | 18.0 µs | 4,336 | 50 |

Treat these as an example of the benchmark output, not a performance promise: the workload is a tiny local `const` action and does not include provider I/O or contention.

## Choose the right state lifetime

### Process-local memory

Modifiers such as `:retry`, `:timeout`, `:concurrency`, `:rate_limit`, `:circuit_breaker`, `:cache`, `:dedup`, `:coalesce`, and `:idempotent` protect actions in the current process/registry. Breaker history, rate-limit tokens, concurrency slots, cached responses, in-flight work, and default idempotency records are local and are lost on restart. A named breaker key shares state only among actions using the same registry/configuration.

`:rate_limit=50` means a local token bucket configured for 50 requests/second with a burst of 50; it is not a fleet-wide quota. `:concurrency=1` limits local in-flight calls and rejects excess calls rather than acting as a distributed queue or lock.

### Durable replay after restart

The CLI can journal completed JSON-compatible step results. Use a stable execution ID and the same source/callsite layout to replay a run:

```sh
nflow run extensions/resilience/nflows/checkout_profile.nflow \
  --journal-dir .flow-state --execution-id checkout-SO-2048
# Run the exact same command again to replay completed recorded steps.
```

Use a new execution ID after changing the workflow. A journal is not a transaction around a remote write, a snapshot of arbitrary Go memory, or proof that an ambiguous request did not reach the provider. For external writes, keep a stable provider/database idempotency key and a reconciliation/manual-recovery path. `FileJournal` persists completed responses, but active claims are instance-local.

### Horizontally scalable workers

NFlow has no modifier that turns local state into shared state. A Go host must inject backends with the required atomic contract: a shared `Journal`, an idempotency coordinator, a distributed `action.RateLimiter`, shared cache layers, or a lock service. Kernel supports distributed limiting and renewable fenced locks, but Flow does not ship a Redis/database implementation. For a critical shared write, the protected data store must reject stale fencing tokens; a basic TTL lock is not enough. `FileJournal`, `:concurrency=1`, and `:idempotent` are not substitutes for distributed coordination.

## Kernel resilience inventory

“Local” means state belongs to an action, bundle, or process instance unless the host injects a shared implementation. “Host-only” means Kernel has an API, but NFlow has no corresponding policy syntax today.

| # | Kernel pattern | NFlow support and practical boundary |
|---:|---|---|
| 1 | Retry execution/count | `:retry` or `:retry=N`; N retries follow the initial call. Use for transient failures and repeatable operations. |
| 2 | Backoff and jitter | Configurable `constant`, `exponential`, and `exponential_jitter`. Linear backoff is host-only; jitter helps avoid synchronized retry surges. |
| 3 | Retry error predicate | Default is Kernel’s transient-error test; `retry_all: "true"` retries permanent errors too. Custom typed predicates are host-only. |
| 4 | Per-attempt timeout | `:timeout=duration` cancels an attempt. It is not a total retry/workflow deadline; propagate the caller’s deadline. |
| 5 | Concurrency bulkhead | `:concurrency=N` rejects excess calls instead of queueing. The limit is local, not per cluster. |
| 6 | Circuit breaker | `@require resilience` plus `:circuit_breaker` / `:circuit_breaker=key`. Named keys share state only within the same registry/config; one probe is admitted after the reset window. |
| 7 | Adaptive breaker/timeout | Kernel has adaptive and smart-resilience policies; no NFlow modifier/config option exposes them. Host-only. |
| 8 | In-memory rate limit | `:rate_limit=N` is a local token bucket at N requests/second, burst N. |
| 9 | Distributed rate limit | Kernel accepts an injected `action.RateLimiter`; NFlow cannot select a shared backend or key function. Host-only wiring. |
| 10 | Adaptive load shedding | Kernel can shed by priority using host runtime statistics; no NFlow modifier. Host-only. |
| 11 | Admission control | Kernel can acquire/release an external permit; no NFlow admission policy. The host must bound acquisition and make release cancellation-safe. |
| 12 | Delayed hedging | Kernel can launch a delayed duplicate and cancel/join the loser; no NFlow hedge modifier. Use only for safe-to-repeat reads. |
| 13 | In-flight deduplication | `:dedup` shares an identical in-flight request locally; it is not a durable response cache. |
| 14 | Request coalescing | `:coalesce` shares in-flight work locally. A Kernel host can share a coalescer across actions; NFlow’s modifier does not. |
| 15 | Response cache / `Once()` | `:cache=duration` caches successful results locally. Kernel cache layers can be composed by a host; `Builder.Once()` is host-only and also memoizes an error. |
| 16 | In-memory idempotency | `:idempotent` uses local state by default and checks request-payload reuse. It is not the provider’s idempotency key. |
| 17 | Persistent idempotency responses/leases | A host can inject `runner.Config.IdempotencyStore`; `FileJournal` persists responses, but active claims are instance-local. Shared workers need an atomic coordinator. |
| 18 | Durable journal/replay | CLI `--journal-dir` or `runner.Config.Journal` replays successful JSON step results. It is not a workflow lock, rollback, or arbitrary Go-memory snapshot. |
| 19 | Resume-identity validation | Kernel has explicit identity validation; Flow’s ordinary journal wiring does not expose it. Preserve source/callsites and use a new ID after a change. |
| 20 | Exclusive execution/leader-only | Kernel supports host-provided locks; NFlow has no lock modifier. Basic TTL locks do not fence stale writers. |
| 21 | Renewable fenced leases | Kernel supports fenced leases; no NFlow modifier. The storage being changed must enforce monotonically increasing fencing tokens. |
| 22 | Graceful drain/in-flight tracking | Kernel has a drain supervisor; no generic NFlow drain modifier. Hosts must stop admission, propagate cancellation, and wait for active work. |
| 23 | Saga compensation | Kernel has an in-memory saga API; there is no Saga DSL or crash-durable orchestration. Persist state externally; make steps and compensation idempotent. |

The built-in `nflow self bench resilience` suite measures NFlow-exposed paths: retry/count, constant backoff, the `retry_all` error-policy branch, per-attempt timeout, an uncontended concurrency bulkhead, an open circuit breaker, a non-depleting local rate limit, sequential deduplication/coalescing middleware, warm response-cache and in-memory-idempotency hits, sequential fallback, typed recovery, and `race`. The `race` scenario is **not** a delayed-hedging benchmark. This suite does not claim to measure timeout cancellation, contention, rate-limit throttling, concurrent deduplication/coalescing, default retry filtering, or alternative exponential/jitter backoffs.

No NFlow performance measurement is provided for Kernel patterns without an NFlow policy surface: adaptive/smart resilience and adaptive or priority-based load shedding; external admission permits; distributed rate limiting; delayed hedging; shared locks, leader-only execution, and renewable fenced leases; generic graceful drain; Saga orchestration; custom typed retry predicates and linear backoff; `Builder.Once()`; explicit resume-identity validation; or custom cache-layer selection. Persistent idempotency stores and durable journal/replay are available through Go-host or CLI configuration, not as per-flow policy, so they are also excluded from this NFlow-only suite. See [the embedded benchmark notes](../cli/selfbench/scenarios/resilience/README.md) for scenario details.

## Recovery operators are not interchangeable

- `left || right` is a sequential fallback for an ordinary error. It can catch permanent errors too; use typed recovery when only selected failures should recover. Caller cancellation remains terminal.
- `? on_error { ... }` handles one expression; `@on_error { ... }` handles a pipeline. Match typed kinds and use `error.reraise` for errors that must remain failures.
- `race { ... }` starts candidates concurrently and returns the first successful result. It is not delayed hedging: it launches all candidates immediately, can multiply traffic, and is appropriate only for safe-to-repeat work. Ensure losers honor cancellation.

## Before production

For each dependency, choose a profile that matches its operation: bounded retry/timeout for transient read failures; a named breaker where isolation is useful; a cache only when stale/old results are acceptable; and write retries only when the external system honors a stable idempotency key. Set the retry budget inside the caller’s overall deadline. Decide whether each state is process-local, durable, or shared, and test cancellation, restart, duplicate execution IDs, and ambiguous/partial side effects. A cached fallback or recorded step is not proof that an external write did not happen.

For implementation details, see the [resilience extension](../extensions/resilience/), [core modifiers](../extensions/modifiers_core/), [error handling](on-error.md), and [durable runner guide](runner-durability.md).
