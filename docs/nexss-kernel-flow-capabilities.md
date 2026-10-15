# Kernel ↔ Flow Capability Map and Reuse Guide

**Source audit verified:** 2026-10-15
**Kernel source basis:** published `v0.33.0`, commit `8d35ba629b181e260337948acb4a4e07f7505275`
**Flow source basis:** published `v0.31.3`, commit `a9156b2c0194b0edafdc6899f786ed22a32f5e3b`
**Candidate context:** Flow changes are compared with published `v0.31.3`; the Kernel follow-up is compared with published `v0.33.0`. Historical release-specific inventory statements below retain their explicitly cited earlier baselines.

This document inventories the Kernel and Flow framework modules only. It distinguishes framework-owned semantics from Flow’s DSL/adapters and from capabilities that must be supplied by an embedding host. Examples and specification bundles are identified as examples, not as public framework API. It does not document application products.

## 1. Architecture and ownership

```text
.nflow source
    │
    ▼
Flow compiler ── directives, AST, DSL lowering, bundle composition
    │
    ▼
Flow runner / resolver ── mounts selected actions, stream sources and operators
    │
    ▼
Kernel runtime ── canonical action, policy, stream and execution semantics
    │
    ▼
Embedding host ── concrete actions, providers, identity, credentials,
                  transactions, durable shared stores and process lifecycle
```

| Layer | Owns | Reuse rule |
|---|---|---|
| **Kernel** | Domain-neutral action construction and invocation, middleware and composition, resilience/idempotency/journal/fencing contracts, streams, DAG primitives, context/error contracts, and shared execution helpers. | The canonical semantics live here. Consumers should call Kernel APIs rather than reimplementing the same algorithm. An interface alone does not provide a durable or distributed backend. |
| **Flow** | `.nflow` syntax and AST, compiler and lowering, runner lifecycle, native bundle selection, `@require` composition, and adapters that translate DSL declarations into Kernel APIs. | Flow is an orchestration/DSL layer over Kernel, not a second Kernel. A Flow adapter may own syntax, parsing, identity mapping, defaults, or DSL-specific behavior while delegating execution semantics. |
| **Embedding host / application** | Concrete action implementations, external I/O, provider configuration, authentication and identity, credentials, database/provider transactions, durable shared stores and locks, external-side-effect reconciliation, and process/service lifecycle. | These responsibilities are not implied by a Kernel contract, an NFlow declaration, a local file journal, or the presence of a bundle. The host must construct and mount the capability and supply its real dependencies. |

Flow’s `compiler.Bundle` is a composition contract for directives, modifiers, operators, primary syntax, libraries, materializers, hooks, and fixtures. The compiler resolves the capabilities supplied to it; it does not inspect Kernel and automatically publish every Kernel API into `.nflow`. `native.Bundles()` is a deliberately selected set, not a reflection of the full Kernel API.

## 2. Package and subpackage inventory

The lists below are based on Go source package directories at the pinned commits. Flow `extensions/*/nflows/` directories contain embedded `.nflow` conformance fixtures; they are data within the parent Go package, not additional Go subpackages.

### 2.1 Kernel `github.com/nexssp/kernel`

| Package | Responsibility and reuse surface |
|---|---|
| `action` | Typed actions, builders, metadata, invocation, middleware, composition, transport/library contracts, retries and resilience, cache/coalescing/dedup/idempotency, durable-step journal contracts, fenced-lock contracts, transactions, async/fan-out/race helpers, and action hooks. This is the main reusable runtime surface. |
| `ai/dag` | Local action-oriented DAG construction, validation/compilation into deterministic layers, and execution with cancellation and layer-boundary result merging. |
| `broadcast` | Single-producer/multi-consumer ring-buffer broadcast for publishing state snapshots without blocking the producer. |
| `node` | Reusable node-level execution abstractions used by the Kernel’s action/composition model. |
| `observe` | Observer/sink contracts and adapters for observing action execution without changing the action’s business implementation. |
| `ringbuf` | Bounded ring-buffer primitives for retaining and reading recent events or values. |
| `stream` | Typed, lazy stream sources and operators, including transformations, filtering, batching, folding, timing, and collection. Operators are Go APIs; their existence does not imply Flow DSL registration. |
| `timing` | Demand-driven hashed timing wheel for managing large numbers of short-lived timeouts without allocating one OS timer per timeout. |
| `xctx` | Typed context helpers for cancellation/deadlines and request/execution metadata; application state should remain in explicit action parameters. |
| `xerr` | Categorized errors with stable kinds and conversions for transport/application boundaries while retaining causes. |
| `xfile` | Typed file references and provider/resolver contracts. A concrete provider and its access policy are supplied by the host. |
| `xfs` | Filesystem helpers, including rooted containment for untrusted paths and atomic writes for trusted paths. |
| `xsync` | Strongly typed concurrency primitives. |
| `xtest` | Fatal-by-default test helpers. |
| `xtest/ktest` | Higher-level Kernel action-runtime test helpers and assertions. |

Kernel also ships 13 executable example packages under `examples/`: `01_ai_dag_pipeline`, `02_resilient_action`, `03_composition_and_fanout`, `04_fenced_distributed_lock_failover`, `05_parallel_scatter_gather_recovery`, `06_pure_streaming_pipeline`, `07_hierarchical_nested_dag`, `08_thundering_herd_coalescing`, `09_retry_policies`, `10_observe_lifecycle`, `11_lazy_etl_stream`, `12_stream_action_crossover`, and `13_resilient_mod_host`. These illustrate the API; they are not additional runtime packages or an automatic host configuration.

### 2.2 Flow `github.com/nexssp/flow`

#### Core, compiler, CLI, and runner packages

| Package | Responsibility and reuse surface |
|---|---|
| `builder` | Build/scaffold support used by Flow’s project/build tooling; it is not the NFlow execution engine. |
| `cli` | Command-line handling for the `nflow` tool, including run, build, lint, inspect, and related commands. |
| `cli/selftest` | In-process execution and reporting for embedded bundle conformance fixtures. |
| `cmd/nflow` | Executable entry point for the `nflow` CLI. |
| `compiler` | DSL lexer/parser/AST, directives and extension contracts, bundle registry, capability resolver and mount logic, modifier/operator tables, compilation and lowering to Kernel actions. |
| `compiler/contracts` | Narrow runtime contracts passed through context for bundle behaviors such as action lookup and pool/recovery integration. |
| `compiler/redact` | Conservative redaction of credential-shaped fields before logging. |
| `native` | Single source of truth for the bundle set loaded by the shipped CLI; exposes the standard set and the larger self-test set. |
| `runner` | Host-facing configuration, compilation and execution of `.nflow` source; mounts configured capabilities and carries journal, idempotency, observer, file-resolver, and run options into execution. |

#### Extension packages

| Package | Responsibility and reuse surface |
|---|---|
| `extensions/approval` | Approval declarations and gate actions; approval identity and authorization remain host responsibilities. |
| `extensions/assert` | Assertions over Flow values and results. |
| `extensions/config` | Flow configuration declarations and runtime configuration access. |
| `extensions/config_yaml` | YAML configuration integration; present in the source tree but not part of the ordinary `native.Bundles()` set. |
| `extensions/constants` | Constant declarations and references. |
| `extensions/decide` | Decision-oriented Flow syntax/actions; present in the source tree but not part of the ordinary native set. |
| `extensions/description` | Descriptive metadata for Flow programs and capabilities. |
| `extensions/embed` | Compiles static `@embed` declarations to typed `xfile.Ref` values; it does not make a provider available by itself. |
| `extensions/external` | Bundled external execution actions such as command, HTTP, and WASM calls; their security and lifecycle still depend on the host. |
| `extensions/files` | Validation of static file declarations; it does not retain file bytes, open a provider, or implement runtime storage. |
| `extensions/flow_version` | Flow-version metadata and version-related declarations/checks. |
| `extensions/fs` | Filesystem-facing Flow actions backed by the relevant Kernel/host filesystem capability. |
| `extensions/hook` | `@hook` wiring and hook-related probe support, delegating hook behavior to Kernel’s hook registry/contracts. |
| `extensions/include` | Source inclusion and expansion support. |
| `extensions/io` | Input/output-oriented Flow actions. |
| `extensions/loop` | `loop(body) until(condition)` primary syntax and its lowering to execution composition. |
| `extensions/macros` | Macro declaration and expansion. |
| `extensions/match` | Match primary syntax and predicate/subject handling. |
| `extensions/modifiers_auth` | DSL translation for authorization-related modifiers/metadata; it does not authenticate a caller or supply host claims. |
| `extensions/modifiers_core` | Kernel-owned policy modifiers: timeout, retry, concurrency limit, cache, coalesce, dedup, idempotent, and rate limit. The modifier adapter calls Kernel builder methods. |
| `extensions/modifiers_meta` | Metadata-oriented modifiers that annotate or describe compiled Flow actions. |
| `extensions/nodes_bench` | Benchmark-oriented Flow action/node support. |
| `extensions/nodes_dispatch` | Dispatch-oriented action/node composition. |
| `extensions/nodes_distribute` | `distribute.map` and `distribute.reduce`; the approved candidate changes the map scheduler to delegate bounded fan-out to Kernel. |
| `extensions/nodes_log` | Logging actions for Flow programs. |
| `extensions/nodes_supervisor` | Supervisor-style orchestration exposed to Flow; its lifecycle semantics are distinct from Kernel action/DAG primitives. |
| `extensions/on` | Event/lifecycle `@on` declarations and their compilation. |
| `extensions/on_error` | Error-handler declarations and error-path composition. |
| `extensions/pipeline` | `@pipeline` declarations and pipeline materialization. |
| `extensions/pool` | `@pool` declarations and materialization of callable pool actions. |
| `extensions/progress` | Progress reporting and progress-oriented Flow integration. |
| `extensions/projection` | Projection of values into declared output shapes. |
| `extensions/realtime` | Realtime/event-oriented Flow integration. |
| `extensions/render` | Rendering actions for Flow values. |
| `extensions/require` | `@require` resolution and composition of extension bundles. |
| `extensions/resilience` | Optional `@require resilience` bundle that composes Kernel retry, per-attempt timeout, concurrency, and circuit-breaker policies; see [resilience usage](resilience.md). |
| `extensions/retry` | Flow atom advice that parses retry declarations and configures Kernel retry middleware; retry execution remains Kernel-owned. |
| `extensions/runtime` | Standard runtime actions and facilities available to bundled Flow programs. |
| `extensions/schema` | `@schema`/`:schema` declaration translation, defaults and validation at the declared pipeline invocation boundary. |
| `extensions/scope` | Scope declarations and their compile/runtime application. |
| `extensions/selftestkit` | Test-only actions/stream sources used by extension fixtures; not part of the production native set. |
| `extensions/stream_ops` | Flow adapters for a selected set of Kernel stream operators. The published `v0.30.3` set is listed below. |
| `extensions/symbols` | Symbol/name handling used during compilation and reference resolution. |
| `extensions/syntax` | Core syntax operators (`->`, `|`, `&`, `||`) plus structured parallel and `race` forms, lowered into Kernel composition. |
| `extensions/template` | Template-rendering integration; present in the source tree but not part of the ordinary native set. |

The module also contains the example-only package `examples/05_arena/bundle` and the specification-only bundles `spec/01_order_intake/bundle`, `spec/02_support_triage/bundle`, and `spec/03_reconciliation/bundle`. They demonstrate composed scenarios; they are not additional general-purpose Flow framework packages. `internal/kernelcoverage` contains internal coverage-test assets rather than a public import package.

## 3. NFlow surface, host mounting, and reachability

### 3.1 What the shipped CLI mounts by default

At the published Flow `v0.30.3` source, `native.Bundles()` loads this selected standard set:

```text
syntax, runtime, symbols, match, modifiers_core, modifiers_auth,
modifiers_meta, pipeline, pool, config, constants, flow_version, schema,
projection, progress, description, include, hook, macros, on, require, loop,
assert, approval, external, fs, io, render, on_error, retry, nodes_log,
nodes_bench, nodes_distribute, nodes_dispatch, nodes_supervisor, scope,
realtime, stream_ops
```

`native.SelftestBundles()` adds `selftestkit`, `config_yaml`, `decide`, and `template` for conformance runs; this is not a statement that those bundles are automatically in the ordinary CLI execution set. `embed` and `files` are not in the standard native set; a custom embedding must import/build and mount the relevant bundle. A host can expose other bundles through `@require` only when the bundle code is present and registered in that host binary, or by explicitly assembling the compiler/runner configuration. An arbitrary Kernel action is not made available in `.nflow` merely because the application imports Kernel.

### 3.2 Confirmed stock DSL reachability at `v0.30.3`

| Capability | Published NFlow reachability | Boundary/qualification |
|---|---|---|
| Core action modifiers | Selected `:timeout`, `:retry`, `:concurrency`, `:cache`, `:coalesce`, `:dedup`, `:idempotent`, and `:rate_limit` modifiers are registered. | These are Flow syntax adapters to Kernel builder policies. Their availability does not prove that a host supplied a durable store, distributed lock, admission controller, or provider policy. |
| Retry advice | `:retry=N` is parsed by Flow’s retry adviser, which installs Kernel retry middleware using the selected Flow policy/defaults. | Retry predicate closures (`RetryIf`) and richer host retry/admission policy are not automatically represented by this syntax. |
| Composition/control syntax | Pipeline declarations, `->`/`|`, `&`, `||` fallback, named parallel/race forms, pools, match, and loop are represented by the stock syntax/bundles. | Flow owns the syntax and lowering; the corresponding action execution/composition is delegated to Kernel where that is the canonical primitive. `||` is fallback, not the `race` keyword. |
| Schema | `@schema` and pipeline `:schema=Name` apply schema defaults and validation at the pipeline invocation boundary. | This is Flow’s schema translation/boundary adapter; do not duplicate the same defaults in every consuming action. |
| Hooks | `@hook` connects named hooks from the Kernel registry to compiled pipeline execution. | Hook registration and actual host observations are not inferred from the declaration. |
| Stock stream operators | `stream.batch`, `stream.fold`, `stream.throttle`, `stream.window`, `stream.take`, and `stream.collect` are registered in `extensions/stream_ops/operators.go`. | This is a selected subset, not a mirror of the Kernel `stream` package. |
| Distribute actions | `distribute.map` and `distribute.reduce` are bundled. | In published `v0.30.3`, `distribute.map` contains its own bounded scheduler; the approved candidate replaces that scheduler with Kernel `action.FanOut` delegation. |

### 3.3 Kernel capabilities that are not automatically reachable from stock `.nflow`

The following are reachability gaps or host wiring points—not evidence of duplicate implementations and not automatic mandates to add DSL syntax:

| Kernel capability / surface | `v0.30.3` status in `.nflow` | Correct boundary |
|---|---|---|
| `stream.Map`, `stream.Filter`, `stream.FlatMap` | Present in Kernel `stream`; not registered by published Flow `extensions/stream_ops`. | The approved cumulative candidate adds thin `stream.map`, `stream.filter`, and `stream.flatMap` delegations with fixtures/tests. It must call Kernel rather than reimplement operators. |
| `stream.Debounce`, `stream.Distinct`/`DistinctBy` | Kernel Go APIs; not part of the stock `stream_ops` registration listed above. | Keep Go/host-only until ordering, backpressure, keying, and error semantics have a concrete NFlow contract and tests. |
| `RetryIf` | Kernel builder API; no stock DSL declaration supplies its predicate function. | Host/Go configuration or a future adapter only for a concrete declarative predicate contract; do not invent a DSL form that cannot express the intended predicate safely. |
| Circuit-breaker/adaptive load shedding/admission and full concurrency controls | Kernel has Go-level resilience/admission primitives; the stock `:concurrency` modifier is not a complete adaptive admission policy. | Keep policies and concrete admission providers at the host boundary until the required lifecycle and semantics are explicit. |
| Saga and hedge composition | Kernel Go APIs exist; no stock `.nflow` syntax automatically exposes them. | Host/Go composition today; consider syntax only for a tested, concrete use case with side-effect and compensation rules. |
| Fenced distributed lock | Kernel defines `FencedMutex`/lease contracts, but contracts do not provision a shared durable lock service. | Host must supply the production backend and propagate fencing tokens to downstream systems that can reject stale writers. |
| `TxRunner` | Kernel exposes a Go transaction contract; no general NFlow transaction provider is implied. | Database/provider transaction selection and transaction lifetime remain host-owned. |
| Idempotency key/store/lease configuration | `:idempotent` is a DSL marker, while store/coordinator/lease configuration is supplied through runner/host configuration. | The host supplies the store and stable execution identity; do not infer distributed idempotency from the modifier alone. |
| Journal and resume configuration | Flow runner supports journal/run configuration, including a file-journal option and execution IDs; the DSL does not define a general job lifecycle. | Configure via the runner/host and use stable execution IDs when replay is intended. This is step replay, not enqueue/claim/worker/result service. |
| Advanced DAG/async, raw context, observer selection | Kernel APIs/contracts exist, but they are not all exposed as stock `.nflow` declarations. | Use Flow’s existing syntax where appropriate; otherwise mount a host bundle or call the Go API. Preserve context and observer ownership at the host/runtime boundary. |
| Provider-backed `xfile` | Kernel defines typed file-reference/provider contracts. | The host/bundle must construct and mount the provider and authorize access; a reference or `@embed` declaration does not create the provider. |

Recommended practice: maintain a tested **NFlow → Kernel/host capability map** for every exposed bundle. Add a DSL surface only after there is (1) a concrete use case, (2) a real `.nflow` example, and (3) a test proving that the adapter delegates to the canonical Kernel API and preserves its semantics.

## 4. Durability, idempotency, and delivery caveats

Kernel’s `action.Journal` records completed step results for replay under an execution ID and step name. Kernel’s idempotency contracts record/reuse action responses and may support claim/complete/lease-renewal coordination. These are contracts and middleware—not a generic job service.

Flow `runner.FileJournal` is a local file-backed adapter for Kernel durable-step records and idempotency responses. In the pinned source it implements the Kernel journal/idempotency contracts and is protected by an in-process mutex; its own source explicitly says its idempotency claims are not a distributed lock across separate processes. Runner options can select a journal directory and execution ID. A Flow run with a journal can replay completed steps; that does **not** provide a generic enqueue → claim → execute → result lifecycle, a shared worker queue, or cross-process fencing.

Kernel’s in-memory stores and memory mutexes are process-local defaults/examples. `FencedMutex` is an interface: distributed safety requires a real host implementation with monotonic fencing tokens and downstream enforcement. A configured local journal or a registered action does not become a shared lock or durable job queue by naming convention.

Exactly-once effects across an external side effect and a later journal/idempotency write cannot be claimed: a process can fail in the interval between the provider effect and durable recording. Use provider-supported idempotency keys and/or host-owned transactional outbox/reconciliation semantics, and document the actual guarantee. Database/provider transaction boundaries, credentials, identity, external-effect fencing, and service lifecycle remain host responsibilities.

## 5. Evidence-based duplication audit

An overlap in names or responsibilities is not, by itself, a duplicate. DSL syntax, adapters, boundary translation, and distinct lifecycle semantics are valid layers when they delegate to the canonical implementation.

| Area | Finding | Classification / action |
|---|---|---|
| Bounded `distribute.map` scheduler vs Kernel `action.FanOut` | The published Flow `v0.30.3` map implementation owns bounded goroutine scheduling and indexed results even though Kernel `FanOut` already provides bounded concurrent action execution. | **Only confirmed safe duplicate.** The approved candidate delegates the scheduler to `action.FanOut` and preserves ordered results, per-item errors, fail-fast behavior, and cancellation before invocation. No public API, DSL syntax, or Kernel change is part of this correction. |
| Retry parsing and lowering | Flow parses/consumes `:retry` and maps its selected policy to Kernel builder methods. | Adapter and DSL-specific defaults, not a second retry algorithm. Preserve the adapter; do not move retry execution semantics into Flow. |
| Runner journaling and idempotency scope | Flow maps source positions/static call sites and repeated occurrences to Kernel journal/idempotency contracts and supplies `FileJournal`. | Runner identity and file-provider adapter, not a duplicate Kernel journal algorithm or a job queue. Preserve the separation and state the local/process boundary. |
| Schema declarations/defaults | Flow parses schema declarations and applies defaults/validation at the declared pipeline invocation boundary using its validation integration. | DSL/schema translation and boundary behavior, not a duplicate action policy. Keep defaults at the boundary that owns the contract. |
| Compiler, AST, DSL composition, and DAG | Flow owns NFlow parsing/compiler/lowering; Kernel owns reusable action/DAG runtime primitives. | Different responsibilities. Syntax/compiler composition is not a copied Kernel DAG implementation. |
| Supervisor and lifecycle integration | Flow’s supervisor extension provides Flow-specific orchestration; Kernel action composition and DAG APIs are lower-level reusable mechanisms. | Distinct semantics unless a specific code path is proven to reimplement the same scheduler or state machine. Do not label overlap alone a defect. |

## 6. Flow repository Taskfile conventions

The following names and semantics are verified specifically in the Flow `Taskfile.yml` at commit `d7b1487d8fbe5273ad8c91b85282f040c8df229b`. **They are Flow-repository conventions, not universal NexSSP repository conventions.** Check the target repository’s own Taskfile before reusing these commands.

- `PKG` is `{{.CLI_ARGS | default "./..."}}`.
- `task test -- <pkg>` runs `go test -short <pkg>`; `task test:nocache -- <pkg>` runs `go test -count=1 <pkg>`.
- The same package argument scopes `test:race`, `test:race:nocache`, `test:cover`, `bench`, `build`, and `vuln`. `test:race` adds `-race -short`; `test:race:nocache` adds `-race -count=1`; `test:cover` uses a coverage profile and `-short`; `bench` runs benchmarks; `build` runs `go build`; and `vuln` runs `govulncheck`.
- `task lint` runs repository-wide `golangci-lint run` and ignores `CLI_ARGS`; `task lint -- <pkg>` is **not** package-scoped lint. For a package-scoped lint run use `golangci-lint run <pkg>` directly.
- `task ci` runs the full-repository `lint` and `test:race:nocache` targets in parallel with fail-fast behavior.
- `task pre-commit` depends on `self`, `lint`, and `test`; `task pre-push` depends on `self`, `lint`, and `test:race`.
- `task self` runs `nflow self test`.
- `task lint:nflow` lints `./examples` and `./spec` with the NFlow CLI.
- `task fmt` runs `golangci-lint fmt`.

## 7. Candidate status and roadmap

### Current private resilience candidate

The current private candidate is based on published Flow `v0.31.3` commit `f120e46da4844d4a35b197494de362ddf91da69f` and published Kernel `v0.33.0`. It adds an optional `@require resilience` bundle for retry, per-attempt timeout, concurrency, and circuit-breaker policies. Generated-binary selection loads resilience for `@require resilience` or `:circuit_breaker`, avoids mounting the legacy retry adviser when resilience owns `:retry`, and leaves retry-only sources on the legacy retry bundle. The candidate is not merged or released; the post-rebase candidate has not been validated.

### Earlier cumulative candidate (historical)

The following scope describes an earlier cumulative Flow `v0.31.0` candidate based on published Flow `v0.30.3`; it is not the current resilience patch:

1. Replace the bounded scheduler inside `extensions/nodes_distribute/map.go` with delegation to Kernel `action.FanOut`, preserving ordered results, per-item errors, fail-fast behavior, and pre-invocation cancellation.
2. Add `stream.map`, `stream.filter`, and `stream.flatMap` as thin delegations to Kernel `stream.Map`, `stream.Filter`, and `stream.FlatMap`, with NFlow fixtures/tests proving the DSL reaches those canonical APIs.
3. Include this Flow-only capability-map document in the same cumulative candidate.

The earlier candidate's validation record reports that scoped lint, Flow `task lint`, affected-package focused/no-cache and race/no-cache tests, full `task test:nocache`, and `task ci` passed on Go 1.26.9. Those results apply only to that earlier code candidate.

### Later-stage ideas — not implemented or approved by this document

- Expose `RetryIf`, Adaptive admission/load-shedding, and any additional concurrency policy only after specifying NFlow semantics and a real use case; circuit-breaker exposure is implemented in the current private resilience candidate.
- Clarify idempotency/journal/resume configuration around stable execution IDs and host-supplied stores/fencing; do not promise a generic durable job lifecycle.
- Consider `saga`, `hedge`, `TxRunner`, or fencing syntax only for concrete scenarios with explicit side-effect, compensation, host-capability, and provider-lifecycle contracts.
- Defer `stream.distinct`/`stream.debounce` until ordering, backpressure, cancellation, and error semantics are explicit.

Every later exposure should include a real `.nflow` example and a test that proves delegation to the canonical Kernel behavior. This roadmap is not authorization to implement those later stages.

## 8. Source references

- [Kernel `v0.33.0` source at `8d35ba629b181e260337948acb4a4e07f7505275`](https://github.com/nexssp/kernel/tree/8d35ba629b181e260337948acb4a4e07f7505275) — especially `action/`, `stream/`, `ai/dag/`, `observe/`, `xctx/`, `xerr/`, `xfile/`, and `xfs/`.
- [Flow `v0.30.3` source at `d7b1487d8fbe5273ad8c91b85282f040c8df229b`](https://github.com/nexssp/flow/tree/d7b1487d8fbe5273ad8c91b85282f040c8df229b).
- [Flow `native/native.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/native/native.go) — standard vs self-test bundle sets.
- [Flow `compiler/build.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/compiler/build.go) and [`compiler/resolver_mount.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/compiler/resolver_mount.go) — compile/lowering and explicit resolver mounting.
- [Flow `extensions/stream_ops/operators.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/extensions/stream_ops/operators.go) — published NFlow stream operator registration.
- [Flow `extensions/nodes_distribute/map.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/extensions/nodes_distribute/map.go) — published bounded-map implementation.
- [Flow `runner/file_journal.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/runner/file_journal.go), [`runner/runner.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/runner/runner.go), and [`runner/execution_identity.go`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/runner/execution_identity.go) — runner journal configuration, local file journal, and per-run step identity.
- [Flow `Taskfile.yml`](https://github.com/nexssp/flow/blob/d7b1487d8fbe5273ad8c91b85282f040c8df229b/Taskfile.yml) — repository-specific command semantics.
