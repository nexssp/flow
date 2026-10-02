# AGENTS.md — Nexss Flow

## 1. Kernel-first development

Before adding code, search `github.com/nexssp/kernel`, this repository, and other Nexss packages for an existing primitive. Reuse the existing contract instead of copying it.

Use [`docs/REUSE_INDEX.md`](docs/REUSE_INDEX.md) as the quick navigation catalog, then verify the actual signature in source and tests.

Prefer, in this order:

1. `nexssp/kernel` for domainless runtime behavior;
2. `nexssp/flow` for DSL, compilation, registry, runner, and transport composition;
3. a focused Nexss package (`cost`, `validation`, `transport`, etc.) for its own domain;
4. a Flow extension only when the behavior is genuinely Flow/domain-specific.

Use Kernel primitives whenever available:

- `action.New`, `action.NewStream`, `action.NewOperator`;
- `action.Dynamic`, `action.InvokeAny`, typed action builders;
- `action.Registry` and `action.Library`;
- Kernel retry, timeout, cache, coalescing, streams, and composition;
- `xerr` for classified errors;
- `xctx` for typed context values and async scope isolation;
- `xfs` for path validation, rooted filesystem access, and atomic writes;
- `observe.Hook` and `observe.Sink` for lifecycle telemetry;
- `stream` and `ai/dag` instead of local stream/DAG implementations;
- `xtest` / `xtest/ktest` instead of local assertion or synchronization helpers.

Do not duplicate Kernel behavior in Flow nodes, runner code, transports, or extensions. If Kernel is missing a reusable domainless primitive, propose the smallest Kernel change first.

## 2. Architecture boundaries

- `compiler/` owns parsing, AST, compilation, and compiler contracts.
- `compiler/extensions/` owns pluggable Flow language features and `@require` integration.
- `nodes/` owns Flow-facing actions and domain adapters; runtime mechanics belong to Kernel.
- `runner/` owns Flow execution assembly, capability resolution, lifecycle wiring, and cleanup.
- `transport/` owns transport abstractions and bindings; concrete transports remain separate packages.
- `contracts/` owns execution-context bridges between runner and extensions.
- `nexssp/ai`, `nexssp/cost`, and other sibling packages own their domain logic.

Do not put AI, filesystem, provider, transport, or cost policy into the compiler core or Kernel core.

## 3. Extension rule

Every new DSL feature must use the existing extension/bundle mechanism. Use the appropriate contribution point rather than modifying unrelated compiler code:

- directive: directive extension;
- modifier: modifier extension;
- primary/keyword: primary extension;
- reusable action/stream/operator: Kernel `action.Library`;
- compile-time advice: existing compiler advisor contract;
- pipeline behavior: existing wrapper/materializer contract;
- external capability: `@require` and a bundle package.

A new extension must include wiring tests and a real `.nflow` fixture where possible. Do not create a second registry, parser, runner, or hook system.

## 4. Duplicate and overwrite policy

Nothing silently overwrites or silently loses a registration.

- Duplicate action, stream, operator, directive, modifier, primary, bundle, transport, or binding names must return an error or fail loudly at construction.
- Missing canonical targets must return an error.
- Invalid options must return an error; never silently use a default when the user supplied an invalid value.
- `Library.Overrides` is the only explicit shadowing mechanism and must be limited to documented test/harness use.
- Do not add new short aliases to `action.Library.Aliases`.
- Prefer one canonical, qualified name such as `fs.read`, `transport.request`, or `ai.route`.
- Existing aliases must never shadow canonical names. New code must not depend on aliases.
- `@require ... as name` is a file-local namespace choice and is acceptable; it must not mutate the source library or global registry.

When changing registration code, add tests for:

1. duplicate canonical names;
2. duplicate aliases;
3. alias versus canonical collision;
4. alias pointing to a missing canonical name;
5. repeated mounts;
6. caller-owned library remaining unchanged.

## 5. Errors and context

- Compile/preprocess errors: `core.SourceError` with source file and line.
- Runtime/user errors: `xerr` (`BadRequest`, `Validation`, `NotFound`, `Forbidden`, `Conflict`, `Unavailable`, `Timeout`, `RateLimit`, `Internal`, as appropriate).
- Do not leak bare `errors.New` or unclassified `fmt.Errorf` across package boundaries.
- Context keys must use `xctx.NewKey[T]`; never string keys.
- Preserve trace, tenant, user, scope, and execution metadata.
- For work that outlives the request, use `xctx.CloneForAsync(ctx)` when the child can access or modify request scope.
- Always propagate cancellation and close resources deterministically.

## 6. Filesystem and external execution

- Validate untrusted paths with Kernel `xfs` and rooted filesystem APIs.
- Reject absolute paths, `..` traversal, and symlinks escaping the permitted root.
- Use `xfs.WriteFileAtomic` for durable state.
- Do not put Sandbox-specific behavior into generic `@embed`; embedding is a compile-time asset feature, while Sandbox is one possible consumer.
- External processes, WASM, HTTP, NATS, databases, and provider calls belong in adapters/extensions, not compiler core.

## 7. Performance and concurrency

- Keep hot paths typed and avoid reflection after build time.
- Bound fan-out and worker creation.
- Do not add goroutines without a clear cancellation and cleanup path.
- Measure allocations before optimizing; do not claim zero allocations without benchmarks.
- Reuse Kernel composition, retry, timeout, cache, coalescing, and stream primitives.
- Avoid global mutable state and package-level execution registries.

## 8. Tests and validation

Use `xtest` / `xtest/ktest`; do not add `testify` or parallel assertion helpers.

- Do not use `time.Sleep` for synchronization; use deterministic channels, latches, or `xtest.Eventually`.
- Test duplicate registration, cancellation, cleanup, race safety, and error classification.
- Extension tests test extension behavior; Kernel runtime semantics are tested in Kernel.
- Run before delivery:

```sh
go test ./...
go test -race ./...
go vet ./...
golangci-lint run
```

Run relevant `task` targets and every affected `.nflow` example. A green unit suite is not enough when examples or CLI wiring changed.

## 9. Change discipline

- Inspect the current repository and dependency versions before editing.
- Do not apply code from an older Flow architecture without mapping it to the current package layout.
- Keep changes focused and document compatibility assumptions.
- If a core or Kernel change appears necessary, first write a short RFC describing the missing contract, alternatives considered, and why an existing extension/adapter cannot solve it.
- Update documentation and examples when public names, flags, contracts, or registration behavior changes.
