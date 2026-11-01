# Resilience self-bench scenarios

List the embedded scenarios and run just this suite with:

```sh
nflow self bench resilience --list
nflow self bench resilience --iterations 50 --warmup 3
nflow self bench resilience --iterations 50 --warmup 3 --json
```

Each `.nflow` file is a local, deterministic workload discovered automatically by `nflow self bench`; no separate Go registration is needed. The warm measurements include the normal in-process Flow runner path, not CLI startup, provider I/O, or production service latency. The retry and circuit-breaker scenarios intentionally use local failures and recovery. The retry policy uses a bounded single retry and 1ms constant backoff; the breaker trips once, then the reused compiled action exercises its open path. Cache and idempotency samples are warm hits. Timeout and rate-limit samples use successful, non-throttled fast paths. Concurrency, deduplication, and coalescing are measured sequentially, so these report middleware overhead—not contention or shared-work savings.

The Kernel inventory also includes patterns that have no NFlow policy surface. The self-bench suite does not invent measurements for them:

- Adaptive/smart resilience, adaptive load shedding, and priority-based shedding.
- External admission permits and distributed rate limiting.
- Delayed hedging (the NFlow `race` scenario is not delayed hedging).
- Shared locks, leader-only execution, and renewable fenced leases.
- Generic graceful-drain supervision and Saga orchestration.
- Custom retry predicates, linear backoff, `Builder.Once()`, explicit resume-identity validation, and custom cache-layer selection.
- Persistent idempotency stores and durable journal/replay require host or CLI configuration rather than per-flow NFlow policy; they are not measured by this built-in suite.

Compare scenarios on the same machine and toolchain. Increase iterations only when the workload and host have enough time; retries include the configured backoff.
