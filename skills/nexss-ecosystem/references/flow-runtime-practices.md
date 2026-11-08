# Flow runtime and operational practices

Use this reference for questions where a valid `.nflow` program may still have important runtime, safety, or data-boundary caveats. Recheck the linked current Flow docs and active host before relying on a behavior; Flow and its documentation change. The relevant source docs are listed under [current source starting points](../SKILL.md#current-source-starting-points).

## Use the right example sources

- Use `extensions/**/nflows/*.nflow` for focused syntax and extension conformance examples.
- Use `spec/**` for integrated, deterministic acceptance scenarios that combine extension and scenario-local bundle actions. Read each scenario's `spec/README.md` for its test command and behavior.
- Inspect Flow's top-level `examples` directory before calling it a local example source. In the current source it may only contain a README redirect to the central examples repository.
- Do not treat README examples as proof of active-host availability. Verify exact syntax and registration in the active registry, fixture, bundle wiring, and implementation as needed.

The [Flow language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md) describes the current implementation. Its lifecycle is: preprocessing (directives, includes, constants, requirements), parsing against active operator/keyword registries, compilation against active action/modifier registries, then execution of projections, policies, actions, and runtime validation. A Go package or extension folder alone does not load a capability.

## Separate format, lint, and runtime checks

- `nflow fmt --check FILE` checks whether source is already formatted; formatting is not registry validation.
- `nflow lint FILE` runs preprocessing, parsing, and compilation against that invocation's registries. It catches invalid syntax, unknown actions/modifiers, and compile-time errors, but does not execute actions, fetch external payloads, or prove service behavior.
- Use `nflow run FILE` or the repository's documented fixture/test command for runtime evidence. `nflow test ./spec` runs discovered integrated scenarios through the normal Flow execution path and evaluates their `@assert` checks. The test command's discovery is one directory below its target; included helper files are not entrypoints.
- `@test` is mounted only by `nflow test`; do not assume ordinary `nflow run` recognizes it. Prefer documented assertions or test forms for the actual command.
- Fixtures/specs should use deterministic, local actions where possible. Check for external calls, writes, messages, account changes, or other side effects before running an example.
- A file with `@require` may cause the CLI to build/cache a harness linking Go modules. A successful lint/run still depends on the active resolver, module/toolchain requirements, and bundle exports.

See the current [CLI guide](https://github.com/nexssp/flow/blob/main/docs/cli.md) for exact behavior.

## Respect data and validation boundaries

- `@{ ... }` primarily builds action argument data/references; do not assume it is a general-purpose expression block. Use a projection or supported condition for calculations.
- `@func` packages a pure expression-level calculation. It does not call actions, perform I/O, or replace a schema or workflow boundary.
- Request schemas validate and apply defaults at a pipeline input boundary, before the body. A default fills an absent field, not an explicit `null`, `0`, or empty value.
- Response schemas validate the returned value and do not apply defaults. Prefix a schema with `!` to reject unknown fields at that schema boundary; request and response strictness are independent.
- `@config:strict=true` is a runtime check for eligible typed action requests. It does not validate arbitrary maps or `any`, does not validate responses, and is not compile-time proof of dynamic values. It does not replace `@schema`.
- Compile-time `@const` performs textual substitution during preprocessing; it is not runtime interpolation or arithmetic. `@config` provides compile-time configuration metadata, not automatic fields in runtime input. Use pipeline data or runtime capabilities such as `runtime.env` for execution-time values.

See [schemas](https://github.com/nexssp/flow/blob/main/docs/schema.md), [validation](https://github.com/nexssp/flow/blob/main/docs/validation.md), and [compile-time/runtime values](https://github.com/nexssp/flow/blob/main/docs/constants-compile-runtime.md).

## Handle errors deliberately

- `left || right` is broad fallback: the right side runs after any error from the left, not only a selected error kind. Do not use it where only a particular failure is recoverable.
- `expr ? on_error { ... }` handles one expression and supplies an error-environment map; the failing request is under `.input`.
- `@on_error { ... }` wraps a pipeline and supplies its original request to the handler. Do not assume both recovery forms pass the same shape.
- Prefer typed recovery when only selected kinds such as timeout or unavailable should fall back. If no kind matches, let the error propagate unless a deliberate fallback is justified.
- Caller cancellation and standard `context.Canceled` are terminal; the normal recovery handlers do not run for them.

See [Flow error handling](https://github.com/nexssp/flow/blob/main/docs/on-error.md) and the operator section of the [language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md#pipeline-operators-and-control-flow).

## Bound retries and resilience to the operation

- `:retry=N` means up to `N` retries after the initial attempt. Bare retry uses the configured default; the default policy retries errors classified as transient.
- `:timeout=...` limits an attempt, not the full workflow or the sum of all retries. Propagate the caller's overall deadline.
- A retry can repeat side effects. Use it only for repeatable operations or require the remote provider/database to enforce a stable idempotency key; a local `:idempotent` modifier alone is not cross-process protection.
- Put provider-read resilience on a small provider-call pipeline. Avoid applying retries broadly around a workflow containing writes, notifications, or other non-repeatable stages.
- `:concurrency`, `:rate_limit`, caches, breakers, deduplication, and idempotency are generally process/registry-local unless the host injects a shared implementation. A local modifier is not a distributed lock, fleet-wide quota, or durable queue.
- `race { ... }` starts its candidates immediately. It returns the first success and cancels remaining branches; a branch that does not cooperate with cancellation can delay completion. Do not confuse `race` with delayed hedging.

See the current [retry](https://github.com/nexssp/flow/blob/main/docs/retry.md) and [resilience](https://github.com/nexssp/flow/blob/main/docs/resilience.md) docs.

## Treat journals as replay, not transactions or locks

When using CLI `--journal-dir`, preserve the execution ID, source name, input, and source-positioned callsites needed to replay completed steps. Use a new execution ID after changing workflow source/callsites or the intended input. Confirm the current CLI's exact invocation and entrypoint rules.

A `FileJournal` persists completed results and can help resume or replay; it is not an execution-wide lock, distributed lock, transaction around a remote write, rollback, or proof that an ambiguous request did not reach a provider. Results are JSON-encoded and may return with JSON-native types. Do not run the same ID concurrently when duplicate side effects are unsafe. Retain or remove journal data according to an explicit policy.

For an approval pause, start with a journal capable of suspension, then resume with the same source, input, journal, execution ID, and a matching gate token. The token is only a gate-name match: `role` and `reason` are descriptive metadata, not authenticated identity or authorization. An embedding host must authenticate and authorize an approver before supplying a token; do not expose a raw approval flag to untrusted callers as if it proved human approval.

See [runner durability](https://github.com/nexssp/flow/blob/main/docs/runner-durability.md) and [approval pause/resume](https://github.com/nexssp/flow/blob/main/docs/approval-resume.md).

## Keep streams lazy

Stream sources are lazy. Do not materialize items inside a source or operator; `collect` is the explicit boundary that materializes the stream. For a compact aggregate, use `stream.fold` rather than collecting the whole stream first: it runs in one pass with the current accumulator, emits the initial value for an empty stream, and forwards upstream errors.

See [extension stream guidance](https://github.com/nexssp/flow/blob/main/docs/extensions.md#42-stream-sources-and-operators) and [stream folds](https://github.com/nexssp/flow/blob/main/docs/stream-fold.md).
