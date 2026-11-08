# Working in the Nexss Flow repository

Read this reference when changing `github.com/nexssp/flow` or giving repository-specific contribution advice. The live repository rules take precedence: inspect its current [AGENTS.md](https://github.com/nexssp/flow/blob/main/AGENTS.md), [CONTRIBUTING.md](https://github.com/nexssp/flow/blob/main/CONTRIBUTING.md), [reuse index](https://github.com/nexssp/flow/blob/main/docs/REUSE_INDEX.md), [Taskfile](https://github.com/nexssp/flow/blob/main/Taskfile.yml), `go.mod`, and [CI workflow](https://github.com/nexssp/flow/blob/main/.github/workflows/ci.yml) before acting. Use `task --list` to confirm the commands present in the user's checkout; docs and Task targets can drift.

## Reuse and architecture

Search for an existing contract before adding code. Follow this order: Kernel for domainless runtime behavior; Flow for DSL/compiler/registry/runner behavior; a focused Nexss package for its domain; then a Flow extension only for Flow-specific behavior. Use `docs/REUSE_INDEX.md` as the navigation index, then verify the real signature and behavior in current source and tests. Do not duplicate Kernel primitives or introduce a second registry, parser, runner, or hook system.

Keep architecture boundaries intact. The compiler owns language/compiler contracts; extensions own pluggable DSL features and adapters; the runner assembles execution and lifecycle; the builder handles standalone and external-module builds; transports, storage, AI, cost, and provider-specific policy belong in their appropriate packages, not in the compiler core. If a reusable domainless primitive is missing from Kernel, write a short RFC covering the contract, alternatives, and why Flow or an existing extension cannot solve it before proposing a core change.

## Adding Flow or extension features

Use the existing extension/bundle contribution point for a new DSL feature: directives, modifiers, keyword primaries, actions/streams/operators, compiler advice, pipeline wrappers, and external capabilities each have distinct contracts. Check current extension docs and source rather than modifying compiler code by default.

For each new DSL feature, add wiring coverage and a runnable `.nflow` fixture in the extension's `nflows/` directory. Treat those fixtures as the source of truth for user-visible DSL correctness. If behavior can be expressed in `.nflow` and asserted with `@assert:`, test it there rather than only in Go. Use extension `SelfTest()` or direct-action Go tests only for internals, wiring, or behavior a fixture cannot express; explain why the case needs Go.

Registration must fail visibly rather than silently overwrite or lose entries. Prefer one canonical, qualified capability name; do not add new short aliases to `action.Library.Aliases` or make new code depend on existing aliases. When changing registration, test duplicate canonical names, duplicate aliases, alias/canonical collisions, aliases with missing targets, repeated mounts, and preservation of caller-owned libraries.

For composed workflows, choose examples/specs by purpose: extension `nflows/` for one extension's conformance; root `spec/` for deterministic integration across extensions and scenario-local actions; the central examples repository for tutorial walkthroughs. Keep test inputs deterministic and offline. Do not send messages, change accounts, or mutate external systems in fixtures.

## Focused-first verification

Run the smallest checks that cover the change first. Do not run the full repository suite for every edit. Formatting, static validation, tests, runtime execution, and full CI answer different questions.

Use the Go toolchain declared by the current `go.mod` and inspect `task --list` before choosing targets. From a source checkout, `go run ./cmd/nflow` can invoke the CLI without assuming a globally installed binary. `task setup` fetches missing repository templates and installs some test/security tools, but not `golangci-lint`; use the linter version pinned by CI. Inspect setup effects before running it.

### `.nflow` changes

Use the Flow formatter on only the touched files. `nflow fmt --write FILE` writes formatted source; `nflow fmt --check FILE` checks without writing. Then run `nflow lint FILE` to verify preprocessing, syntax, and the active registry. Lint does not execute the flow. For runtime behavior, run the relevant safe fixture or scenario; use `nflow test <scenario-directory>` for deterministic scenarios with `@assert` checks. Run `task lint:nflow` or the current equivalent when its wider coverage is appropriate.

### Go changes

Use the repository's Go formatter (`task fmt` in the current repository workflow) and inspect the diff because formatting can modify files. Then run targeted lint and tests for changed packages. Current documented patterns include:

```sh
task lint -- ./compiler ./runner
task test -- ./compiler -run '^TestParser' -count=1
```

Substitute the affected package and focused test name. For concurrency-sensitive changes, run the corresponding targeted race test if the current Task target supports it. Check Taskfile/CLI help before relying on a task name or syntax.

A focused pass proves only the selected scope. Before a pull request, release, broad refactor, or risk-bearing change, follow current `CONTRIBUTING.md` and CI gates. The current documented broader workflow includes `task pre-push` and `task ci`; CI also checks build, repository gates, and benchmark smoke. `task tidy` changes `go.mod`/`go.sum`, so inspect and commit only intentional dependency changes. Do not claim a full-suite pass after running focused checks only.

For DSL and CLI changes, include the applicable `.nflow` fixture/spec validation in addition to Go tests. Use `nflow test ./spec` or the current focused Task alias for integrated acceptance behavior; `task test` alone runs Go tests and does not execute integrated scenarios. Confirm exact target availability with `task --list`, because contributor docs and task names may change.

## Correctness, errors, and security

Use `compiler.SourceError` for source-positioned compile/preprocess diagnostics. Classify user/runtime errors with Kernel `xerr`; do not leak bare unclassified errors across package boundaries. Define typed context keys with `xctx.NewKey[T]`, propagate cancellation and trace/scope metadata, and use `xctx.CloneForAsync` when asynchronous work must preserve request scope.

Validate untrusted paths with Kernel `xfs` and rooted filesystem APIs. Reject traversal and symlinks escaping the permitted root; use atomic writes for durable state. Keep external processes and provider/network/database behavior in adapters or extensions, not compiler internals. Never add an external dependency without a concrete reason, compatibility check, and license review.

## Performance and review

Keep hot paths typed and avoid reflection after build time. Bound fan-out and worker creation; do not add goroutines without clear cancellation and cleanup. Measure allocations before optimizing and never claim zero allocations without a benchmark. Record Go version and hardware for performance claims. Reuse Kernel primitives and existing `xtest` / `xtest/ktest` helpers; do not add `testify` or parallel local assertion/synchronization helpers.

Before presenting a change, inspect its diff, compatibility impact, errors, cleanup, tests, and scope. Update docs and examples when public names, flags, contracts, or registration behavior change. Use current project formatting and commit conventions; preserve unrelated changes.
