# Nexss ecosystem package catalogue

## Scope and how to read it

This catalogue covers the intended curated set of **16 repositories**; the owner has confirmed that its repository list matches the private scope registry's `defaultRepos` list. The registry itself is not bundled, so future maintainers need authorized access to refresh the intended scope. This confirms repository-set completeness only—not the current package paths, APIs, releases, or Flow-bundle availability. Treat entries as an evidence-linked inventory and recheck those details against current public source before relying on them. Do not publish private registry contents, credentials, or stale version pins.

Source availability does not prove that a module has a published release or that a bundle is available to a particular flow. The notes distinguish Go packages, CLI commands, Flow bundles, examples/specs, and documentation. Package paths are repository-relative unless shown as a full import path. Recheck moving source, toolchain, and release status before relying on an entry. No release tags, versions, or commit hashes are pinned here.

A Flow bundle is available to a flow only when loaded by the host (commonly with `@require`) and registered in that runtime. `nflow catalog` describes capabilities of a particular Flow host; it is not a catalogue of all ecosystem repositories or Go packages.

## Contents

- [Runtime and source tools](#runtime-and-source-tools)
- [AI and protocol integrations](#ai-and-protocol-integrations)
- [Validation, storage, budgets, and reliability](#validation-storage-budgets-and-reliability)
- [Repository tools and language support](#repository-tools-and-language-support)
- [Registry corrections and maintenance](#registry-corrections-and-maintenance)

## Runtime and source tools

### [Nexss Kernel](https://github.com/nexssp/kernel)

Typed Go execution foundation used by Flow and sibling modules. Public Go module/import examples are documented; the README's `go install` command is for the development Task tool, not a Kernel CLI. No Kernel-owned Flow bundle or separate library install/release route is established by the inspected docs.

| Package | What it provides |
|---|---|
| [`action`](https://github.com/nexssp/kernel/tree/main/action) | Typed action construction, lifecycle, middleware, hooks, composition, and execution policies. |
| [`observe`](https://github.com/nexssp/kernel/tree/main/observe) | Optional action lifecycle events and observability sinks. |
| [`ai/dag`](https://github.com/nexssp/kernel/tree/main/ai/dag) | Typed action DAG construction, validation, deterministic layers, and execution. |
| [`xctx`](https://github.com/nexssp/kernel/tree/main/xctx) | Typed context helpers for request, execution, trace, identity, and permissions metadata. |
| [`xerr`](https://github.com/nexssp/kernel/tree/main/xerr) | Categorized errors with stable kinds and preserved causes. |
| [`stream`](https://github.com/nexssp/kernel/tree/main/stream) | Lazy typed stream operators and context-aware iteration. |
| [`ringbuf`](https://github.com/nexssp/kernel/tree/main/ringbuf) | Bounded, lock-free MPMC ring buffer for event/telemetry paths. |

Evidence: [Kernel README/core packages](https://github.com/nexssp/kernel/blob/main/README.md#core-packages) · [module](https://github.com/nexssp/kernel/blob/main/go.mod). Kernel supplies execution semantics; Flow-side bundles/adapters supply the registered capabilities.

### [Nexss Flow / NFlow](https://github.com/nexssp/flow)

Go implementation of the `.nflow` language, runtime, and CLI. The CLI has documented installation and prebuilt release routes; those routes apply to the CLI, not every package or extension in the repository.

| Registry path | Type and purpose |
|---|---|
| [`cmd/nflow`](https://github.com/nexssp/flow/tree/main/cmd/nflow) | CLI command for running, linting, building, and inspecting `.nflow` programs; not an importable library or a bundle. |
| [`examples`](https://github.com/nexssp/flow/tree/main/examples) | README redirect/index to examples in the central Nexss examples repository; not a package. |
| [`spec`](https://github.com/nexssp/flow/tree/main/spec) | Integrated acceptance specifications and scenario-local bundles; not a reusable library/bundle. |

Evidence: [README](https://github.com/nexssp/flow/blob/main/README.md) · [release/install docs](https://github.com/nexssp/flow/blob/main/docs/releases.md) · [extension guide](https://github.com/nexssp/flow/blob/main/extensions/README.md) · [spec README](https://github.com/nexssp/flow/blob/main/spec/README.md).

### [srcpack](https://github.com/nexssp/srcpack)

Go CLI for selecting local or public GitHub source and packaging focused, source-referenced context for AI/code review. It reads source; it does not edit it. The registry's copied `cmd/nflow`/`spec` entries were incorrect: its CLI is `cmd/srcpack`, and `spec` is absent from current source.

| Package/path | Type and purpose |
|---|---|
| [`cmd/srcpack`](https://github.com/nexssp/srcpack/tree/main/cmd/srcpack) | `srcpack` CLI executable. |
| [`nexssflow`](https://github.com/nexssp/srcpack/tree/main/nexssflow) | Importable Flow adapter with focused and aggregate bundle constructors. |
| [`nflows`](https://github.com/nexssp/srcpack/tree/main/nflows) | Embedded `.nflow` sources and loader API. |
| [`bundles/pack`](https://github.com/nexssp/srcpack/tree/main/bundles/pack) | Focused source-packaging Flow bundle and implementation. |
| [`examples/flow-fold`](https://github.com/nexssp/srcpack/tree/main/examples/flow-fold) | Runnable demonstration, not a library or production CLI. |

The README documents a Go install route; the repository also has a tag-triggered release workflow, but that alone does not verify a current release asset. Evidence: [README](https://github.com/nexssp/srcpack/blob/main/README.md) · [NFlow adapter](https://github.com/nexssp/srcpack/blob/main/nexssflow/library.go) · [embedded loader](https://github.com/nexssp/srcpack/blob/main/nflows/nflows.go) · [release workflow](https://github.com/nexssp/srcpack/blob/main/.github/workflows/release.yml).

## AI and protocol integrations

### [Nexss AI](https://github.com/nexssp/ai)

Public Go module for provider integrations and a Flow adapter. Provider, embedder, index, and memory services are supplied by the host; the repository does not guarantee a provider account or model is configured. README documents branch-based Go/Flow consumption; a published release was not established. No CLI is listed.

| Package | What it provides |
|---|---|
| [`engine/llm`](https://github.com/nexssp/ai/tree/main/engine/llm) | Provider-neutral LLM APIs and provider implementations; includes a Kernel action adapter. |
| [`gateway`](https://github.com/nexssp/ai/tree/main/gateway) | Host-configured provider routing and pre-dispatch gates. |
| [`retrieval`](https://github.com/nexssp/ai/tree/main/retrieval) | Namespace-aware vector retrieval/RAG over host-injected embedder and index implementations. |
| [`nexssflow`](https://github.com/nexssp/ai/tree/main/nexssflow) | Flow bundle registering typed AI actions. |
| [`memory`](https://github.com/nexssp/ai/tree/main/memory) | Host-side hybrid conversation-memory service with explicit storage/model adapters. |
| [`extensions/ai_assist`](https://github.com/nexssp/ai/tree/main/extensions/ai_assist) | Experimental hook/extension; its error-analysis implementation is a TODO, not a working AI analysis feature. |

Evidence: [README](https://github.com/nexssp/ai/blob/main/README.md) · [Flow guide](https://github.com/nexssp/ai/blob/main/nflows/README.md) · [module](https://github.com/nexssp/ai/blob/main/go.mod).

### [Nexss Transport](https://github.com/nexssp/transport)

Go adapters that mount Kernel actions on protocols and schedulers. README documents Go module consumption; several adapters also contain distinct Flow bundles. The task queue and bus described here are in-process/in-memory, not external durable services.

| Package | What it provides |
|---|---|
| [`thttp`](https://github.com/nexssp/transport/tree/main/thttp) | HTTP routes/handlers, middleware, SSE, and NDJSON streaming. |
| [`tcli`](https://github.com/nexssp/transport/tree/main/tcli) | CLI adapter that maps actions to commands/flags; it is not a standalone CLI executable. |
| [`tbus`](https://github.com/nexssp/transport/tree/main/tbus) | In-process publish/subscribe. |
| [`tworker`](https://github.com/nexssp/transport/tree/main/tworker) | Interval workers that execute bound actions. |
| [`cron`](https://github.com/nexssp/transport/tree/main/cron) | Cron/interval scheduling for bound actions. |
| [`bus/taskqueue`](https://github.com/nexssp/transport/tree/main/bus/taskqueue) | In-memory priority queue with workers, retries, and dead-letter handling. |
| [`bus/mediator`](https://github.com/nexssp/transport/tree/main/bus/mediator) | In-process command/event mediator. |

Correct the old registry paths `taskqueue` → `bus/taskqueue` and `mediator` → `bus/mediator`. Flow adapters live below the corresponding package in `nexssflow/`; their presence must be verified for the specific adapter before describing a Flow `@require` route. Evidence: [README](https://github.com/nexssp/transport/blob/main/README.md) · [module](https://github.com/nexssp/transport/blob/main/go.mod).

### [Nexss Transport NATS](https://github.com/nexssp/transportnats)

| Package | What it provides |
|---|---|
| [`tnats`](https://github.com/nexssp/transportnats/tree/main/tnats) | Kernel action adapter for NATS Core, JetStream, KV/ObjectStore, and NATS Micro features. |
| [`nexssflow`](https://github.com/nexssp/transportnats/tree/main/nexssflow) | Separate Flow bundle that uses `tnats`; not a CLI. |

README documents Go module consumption and the Flow bundle; release availability is not established. Evidence: [README](https://github.com/nexssp/transportnats/blob/main/README.md) · [Flow bundle](https://github.com/nexssp/transportnats/blob/main/nexssflow/library.go) · [module](https://github.com/nexssp/transportnats/blob/main/go.mod).

### [Nexss Testkit](https://github.com/nexssp/testkit)

Go testing toolkit; README documents module consumption. No first-party Flow bundle or CLI was identified. Flow appearing as an indirect dependency is not evidence of a Flow integration.

| Package | What it provides |
|---|---|
| [`testkit` (module root)](https://github.com/nexssp/testkit/blob/main/suite.go) | In-process HTTP integration and black-box E2E tests, including SSE, route checks, and load testing. |
| [`rpc`](https://github.com/nexssp/testkit/tree/main/rpc) | In-memory JSON-RPC test client. |
| [`chaos`](https://github.com/nexssp/testkit/tree/main/chaos) | Kernel action middleware/hooks for injecting test delays, errors, or panics. |

Kernel-specific helpers under `kernel/xtest` are separate and are not re-exported by Testkit. Evidence: [README](https://github.com/nexssp/testkit/blob/main/README.md) · [module](https://github.com/nexssp/testkit/blob/main/go.mod).

### [Nexss Transport AI](https://github.com/nexssp/transportai)

A2A and MCP transports that expose actions/flows to protocol clients; not an AI model or agent runtime. README documents Go module and Flow consumption.

| Package | What it provides |
|---|---|
| [`ta2a`](https://github.com/nexssp/transportai/tree/main/ta2a) | A2A-style HTTP transport for Kernel actions. |
| [`tmcp`](https://github.com/nexssp/transportai/tree/main/tmcp) | MCP JSON-RPC transport over stdio or HTTP. |
| [`nexssflow`](https://github.com/nexssp/transportai/tree/main/nexssflow) | Combined Flow bundle for A2A and MCP. Modular adapters also live at `ta2a/nexssflow` and `tmcp/nexssflow`. |

Evidence: [README](https://github.com/nexssp/transportai/blob/main/README.md) · [combined adapter](https://github.com/nexssp/transportai/blob/main/nexssflow/library.go) · [module](https://github.com/nexssp/transportai/blob/main/go.mod).

## Validation, storage, budgets, and reliability

### [Nexss Validation](https://github.com/nexssp/validation)

Go validation libraries with Kernel action helpers. Public module/import usage is documented; no explicit install/release route or Flow bundle was established. No CLI is listed.

| Package | What it provides |
|---|---|
| [`validation` (module root)](https://github.com/nexssp/validation/blob/main/validation.go) | Struct/scalar/map validation, custom rules, structured errors, and Kernel action helpers/hooks. |
| [`validate`](https://github.com/nexssp/validation/tree/main/validate) | Small validation helpers such as required/min/maximum length. |

Evidence: [README](https://github.com/nexssp/validation/blob/main/README.md) · [module](https://github.com/nexssp/validation/blob/main/go.mod).

### [Nexss Storage](https://github.com/nexssp/storage)

Typed storage adapters for Kernel-facing capabilities; README documents Go and Flow consumption. Persistent SQL schemas require explicitly applied migrations; opening a backend does not run DDL.

| Package/path | What it provides |
|---|---|
| [`memory`](https://github.com/nexssp/storage/tree/main/memory) | Process-local reference backends for development/tests; non-durable. |
| [`nexssflow`](https://github.com/nexssp/storage/tree/main/nexssflow) | Flow bundle for store declarations, actions, middleware, and backend selection. |
| [`docs`](https://github.com/nexssp/storage/tree/main/docs) | Markdown documentation, **not** a Go package. |

The public docs also describe persistent backend choices; inspect their current source and migration docs before advising a specific backend. Evidence: [README](https://github.com/nexssp/storage/blob/main/README.md) · [migrations](https://github.com/nexssp/storage/blob/main/docs/migrations.md) · [Flow adapter](https://github.com/nexssp/storage/blob/main/nexssflow/library.go).

### [Nexss Cost](https://github.com/nexssp/cost)

Fixed-point budget reservation and cost accounting. README documents Go module consumption, backend adapters, and Flow use; availability of a particular release was not verified.

| Package | What it provides |
|---|---|
| `.` (module root) | Core types/contracts and in-process atomic ledger. |
| [`adapters/redis`](https://github.com/nexssp/cost/tree/main/adapters/redis) | Distributed reservations/events using Redis. |
| [`adapters/postgres`](https://github.com/nexssp/cost/tree/main/adapters/postgres) | Durable reservations/events through `database/sql`; application chooses driver/migrations. |
| [`adapters/kernel`](https://github.com/nexssp/cost/tree/main/adapters/kernel) | Kernel action hook for reserving/settling budget around actions. |
| [`testkit`](https://github.com/nexssp/cost/tree/main/testkit) | Contract-test helper for custom reserver adapters. |
| [`nexssflow`](https://github.com/nexssp/cost/tree/main/nexssflow) | Flow bundle for budget configuration, estimation, and reporting. |
| `nexssflow/examples/*.nflow` | Sample flows, **not** Go packages or CLI commands. |

Evidence: [README](https://github.com/nexssp/cost/blob/main/README.md) · [Flow adapter](https://github.com/nexssp/cost/blob/main/nexssflow/README.md) · [Kernel adapter](https://github.com/nexssp/cost/blob/main/adapters/kernel/guard.go).

### [Nexss Observability](https://github.com/nexssp/observability)

Go instrumentation for Kernel actions and services. The module proxy has published versions, but that does not prove current `main` matches the latest published version. README and module manifest disagree on the Go toolchain requirement; recheck both before advising installation.

| Package | What it provides |
|---|---|
| [`obs` (module root)](https://github.com/nexssp/observability/blob/main/logger.go) | Provider APIs for tracing, metrics, contextual logging, readiness, and events. |
| [`actionhook`](https://github.com/nexssp/observability/tree/main/actionhook) | Kernel action hook for spans/metrics and lifecycle instrumentation. |
| [`nexssflow`](https://github.com/nexssp/observability/tree/main/nexssflow) | Separate Flow bundle/host adapter; not in the original candidate map. Flow parity is documented as work in progress. |

Evidence: [README](https://github.com/nexssp/observability/blob/main/README.md) · [module](https://github.com/nexssp/observability/blob/main/go.mod) · [Flow adapter docs](https://github.com/nexssp/observability/blob/main/nexssflow/README.md).

### [Nexss Sandbox](https://github.com/nexssp/sandbox)

Sandbox engine contract with Docker, Podman, in-process, and mock backends, plus a Flow adapter. Static policy checks are not an OS security boundary; in-process execution runs directly on the host, and hardened isolation for untrusted workloads is not established.

| Package/path | What it provides |
|---|---|
| `.` (module root) | Engine contract, policies, workspace API, and `Library()` helper. `workspace.go` and `library.go` are files in this package, not subpackages. |
| [`docker`](https://github.com/nexssp/sandbox/tree/main/docker) | Docker-backed engine. |
| [`podman`](https://github.com/nexssp/sandbox/tree/main/podman) | Podman-backed engine. |
| [`nexssflow`](https://github.com/nexssp/sandbox/tree/main/nexssflow) | Flow adapter/bundle exposing generic and named sandbox actions. |
| `examples/` | Flow examples/docs, **not** a package or separate CLI. |

Evidence: [README](https://github.com/nexssp/sandbox/blob/main/README.md) · [Docker backend](https://github.com/nexssp/sandbox/blob/main/docker/docker.go) · [Podman backend](https://github.com/nexssp/sandbox/blob/main/podman/podman.go) · [Flow adapter](https://github.com/nexssp/sandbox/blob/main/nexssflow/library.go).

## Repository tools and language support

### [Nexss Repo](https://github.com/nexssp/repo)

Read-only Go provider and CLI for local Git/GitHub inspection, plus a Flow adapter. README documents source-run CLI examples and Go imports. Verify each command and package against current source; similar names do not establish equivalent behavior or availability.

| Package/path | What it provides |
|---|---|
| `.` (module root) | Typed Provider API for local worktree/GitHub inspection, files, diffs, and bounded source packing. [Source](https://github.com/nexssp/repo/blob/main/repo.go) |
| [`cmd/repo`](https://github.com/nexssp/repo/tree/main/cmd/repo) | JSON CLI for local inspect/files/diff/pack and GitHub inspection. |
| [`cmd/repopeek`](https://github.com/nexssp/repo/tree/main/cmd/repopeek) | Thin GitHub repository overview CLI using the Provider API. |
| [`nexssflow`](https://github.com/nexssp/repo/tree/main/nexssflow) | Flow bundle adapter exposing repository inspection actions. |
| [`nexssflow/nflows/repo.nflow`](https://github.com/nexssp/repo/blob/main/nexssflow/nflows/repo.nflow) | Embedded Flow pipeline definitions, **not** a Go package. |

No `go install` or current published release route was verified; local source-run commands and a Flow bundle are documented. Evidence: [README](https://github.com/nexssp/repo/blob/main/README.md) · [Flow adapter](https://github.com/nexssp/repo/blob/main/nexssflow/README.md).

### [Nexss Lang](https://github.com/nexssp/lang)

Language-runner runtime and source-signature extractor, plus a Python contract-inspection CLI and Flow adapter. The module proxy has published versions, but GitHub Releases is empty; do not assume current `main` is a released version.

| Package/path | What it provides |
|---|---|
| [`core`](https://github.com/nexssp/lang/tree/main/core) | Language-runner profiles, interpreter discovery/installation, source materialization, and process execution. |
| [`extract`](https://github.com/nexssp/lang/tree/main/extract) | Language-neutral source-signature extraction. |
| [`cmd/nexss-lang`](https://github.com/nexssp/lang/tree/main/cmd/nexss-lang) | Static Python dataclass/TypedDict inspection and NFlow schema starter generation. |
| [`nexssflow`](https://github.com/nexssp/lang/tree/main/nexssflow) | Flow adapter/bundle for language runner integration. |

Evidence: [README](https://github.com/nexssp/lang/blob/main/README.md) · [Flow adapter](https://github.com/nexssp/lang/blob/main/nexssflow/README.md) · [module](https://github.com/nexssp/lang/blob/main/go.mod).

### [Nexss OPS](https://github.com/nexssp/ops)

Go developer toolkit for analyzing Go/NFlow source, generating routes/SDKs, and operating Flow programs. README documents Go installation commands; release availability has not been established. No OPS-authored Flow bundle is identified.

| Package/path | What it provides |
|---|---|
| [`analyzer`](https://github.com/nexssp/ops/tree/main/analyzer) | Go/NFlow source analysis and reports/contracts. |
| [`backendgen`](https://github.com/nexssp/ops/tree/main/backendgen) | Generator for HTTP-server bootstrap from explicitly routed actions/pipelines. |
| [`cmd/nexssp`](https://github.com/nexssp/ops/tree/main/cmd/nexssp) | OPS analysis/code-generation CLI. |
| [`cmd/nexss`](https://github.com/nexssp/ops/tree/main/cmd/nexss) | Wrapper entry point for Flow's native bundle set; not an OPS bundle. |
| `examples/studio-incident-triage` | Walkthrough/example, **not** a separate library or bundle. |

Evidence: [README](https://github.com/nexssp/ops/blob/main/README.md) · [analyzer](https://github.com/nexssp/ops/blob/main/analyzer/analyzer.go) · [backend generator](https://github.com/nexssp/ops/blob/main/backendgen/backendgen.go).

## Registry corrections and maintenance

Older package-path metadata is a useful starting point, not verified truth. In particular:

- `srcpack` had a copied Flow description and copied Flow paths. Correct to `cmd/srcpack`; `spec` is absent; `examples` is not an importable package. Use the current srcpack README/source before adding more paths.
- `transport` should use `bus/taskqueue` and `bus/mediator`, not top-level `taskqueue`/`mediator`.
- `storage/docs` is documentation, not a Go package.
- `sandbox/workspace` and `sandbox/library` are not subpackages; those APIs/files belong to the root package.
- `flow/cmd/nflow`, `flow/examples`, and `flow/spec` are respectively a CLI, README redirect/index, and acceptance suite—not reusable Go packages or bundles.
- Some curated repositories lack package-path metadata in older inventories; the principal public surfaces are listed above.
- Several repos provide a `nexssflow` bundle even when older package lists omit it. Include it only when current source documents/registers it.

When refreshing, use the approved scope manifest to keep the intended coverage. Treat package paths as candidates, then verify them against current default-branch source, module manifests, READMEs, tests, and Flow adapter documentation. Correct stale metadata, never copy version pins, and do not expand to every public repository. Give human-facing answers direct package/repository links and distinguish documented installation, Flow integration, and unresolved availability.
