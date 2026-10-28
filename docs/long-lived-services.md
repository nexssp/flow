# Long-lived services in NFlow

`supervisor.run` remains the finite fan-out action for child pipelines. Use the separate `supervisor.serve` action when one NFlow entrypoint owns several services that should stay alive together.

## Declare the service graph

```nflow
@pipeline backend_services
  supervisor.serve @{
    services: [
      { id: "http", action: "thttp.listen", payload: .http_config },
      { id: "worker", action: "tworker.listen", depends_on: ["http"], enabled: .worker_enabled, payload: .worker_config }
    ]
  }
@end
```

The same shape is exercised end-to-end as a self-test fixture at
`extensions/nodes_supervisor/nflows/supervisor_serve.nflow`.

Call `pipeline.backend_services` with `http_config`, `worker_config`, and `worker_enabled`. Service IDs, action names, and dependencies are static and readable in NFlow; `enabled` may be a runtime boolean reference. Omitting `enabled` means enabled. A running service may not depend on a disabled service.

The compiler rejects a missing or non-literal service list, malformed service objects, duplicate IDs, unknown dependencies, dependency cycles, and unknown service fields before execution. The action repeats graph validation at runtime because callers may invoke it with typed values outside the NFlow compiler.

## Readiness and failure behavior

A service starts only after every declared dependency reports ready. Flow resolves every referenced action before starting the first service, then starts independent services concurrently. A service that returns, whether with an error or unexpectedly with `nil`, is a failure; Flow cancels its siblings and returns the first service error. A spontaneous `context.Canceled` from a child is not mistaken for parent-requested shutdown. Cancellation of the parent context is a clean shutdown.

The readiness contract lives in `compiler/contracts`: a long-lived action calls `contracts.SignalReady(ctx)` once its own startup has succeeded. It is a no-op when the action is invoked outside `supervisor.serve`. Current Transport integrations signal after HTTP bind, worker launch, cron schedule registration/start, bus/message subscription setup, mediator registration, or task-queue worker launch. No per-request, per-message, per-cycle, or retry-loop checks are added.

`supervisor.serve` waits for all children to finish after cancellation; it cannot forcibly stop a Go action that ignores its context. Each long-lived action must honor cancellation and complete its existing graceful shutdown. For example, the HTTP adapter performs its existing bounded graceful drain, and the service supervisor joins its action goroutines without introducing another transport cadence or poll loop.

## Cost and compatibility

Readiness is startup-only. The contract test measures zero allocations for repeated `SignalReady` calls after the one-shot signal has fired; that is not a claim that graph setup or the overall service supervisor is allocation-free. Service-graph maps, channels, resolver results, and goroutines are created at service startup, not on steady-state transport paths.

This implementation is based on Flow **v0.49.0** but adds a public contract and action. Do not retag or describe the existing v0.49.0 release as containing them. Release the Flow changes as v0.50.0 first, then publish Transport with Flow v0.50.0 as its minimum dependency, and only then build/release Jumalu against that pair. The coordinated Transport release should be the next compatible minor from its v0.8.0 baseline. The local workspace tests exercise all three source checkouts before those tags exist.
