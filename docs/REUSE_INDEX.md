# Nexss Reuse Index

**Purpose:** the first place to look before adding code.

This is a **navigation aid**, not a replacement for reading the actual
package. Verify signatures against the pinned dependency version before
use. Prefer existing Kernel and Flow primitives over local helpers.

## Priority rule

1. `github.com/nexssp/kernel` — domainless runtime behavior.
2. This repository (`flow/`) — DSL, compiler, runner, registry, harness.
3. A focused Nexss sibling package (`cost`, `validation`, `transport`,
   `ai`, etc.).
4. A new extension under `extensions/<name>/` — only when nothing fits.

Do not copy an existing primitive into an extension. If Kernel is
missing a reusable domainless primitive, propose the smallest Kernel
change first.

---

## Kernel

The Kernel is a separate module (`github.com/nexssp/kernel`) with its
own release cycle. Confirm the pinned version before relying on any
signature. The set below reflects the surface Flow depends on; for the
full API, read the source.

### `kernel/action` — typed actions

| API | Use | Do not reimplement |
|---|---|---|
| `action.New[Req, Res](name, fn)` | Build a typed action | Local action wrappers |
| `action.NewStream[Req, Item](name, fn)` | Lazy stream source (`iter.Seq2`) | Custom source goroutines |
| `action.NewOperator[In, Out, Cfg]` | Typed stream operator | Reflection-based operator registries |
| `action.NewSimpleOperator[In, Out]` | Operator without config | Repeated trivial builders |
| `action.Dynamic(act)` | Bridge a typed action to `any` | Ad-hoc type erasure |
| `action.InvokeAny(ctx, act, req)` | Invoke through the erased boundary | Direct type switches |
| `action.Coerce[T](input)` | Convert dynamic input to a typed `T` | Local reflection decoders |
| `action.Assign(target, source)` | The underlying coerce | — |
| `action.Registry` / `action.Library` | Register and mount capabilities | A second registry |
| `action.CloneWithHooks(hooks...)` | Attach hooks without mutating shared actions | Mutating a shared instance |

Builder methods Flow relies on (all on `*action.Builder[Req, Res]`):

- `Timeout(d)`
- `Retry(n, backoff)`, `RetryIf(n, backoff, pred)`, `RetryAll(n, backoff)`
- `Cache(ttl, keyFn, layers...)`
- `Dedup(keyFn)`, `Coalesce(coalescer, keyFn)`
- `RateLimit(rps, burst)`, `RateLimitWithKey`, `RateLimitDistributed`
- `ConcurrencyLimit(limit)`
- `Idempotent()`, `IdempotentWithConfig(cfg)`
- `Adaptive(cfg)`, `Resilient(cfg)`, `InferredResilient()`
- `RequireAuth()`, `RequireRole`, `RequirePermission`, `RequireFeature`
- `Exclusive`, `ExclusiveFenced`, `LeaderOnly`, `LeaderOnlyFenced`
- `Transactional(runner)`
- `Validate(fn)` — request validation

Backoff constructors: `action.ExponentialBackoff`, `ExponentialJitter`,
`LinearBackoff`, `ConstantBackoff`.

Composition helpers:

- `action.Pipe`, `PipeAny`, `PipeWith`
- `action.Parallel`, `ParallelAny`, `ParallelNamed`, `ParallelMap`
- `action.Branch`, `BranchAny`
- `action.FirstSuccess`, `FirstSuccessAny`
- `action.TernaryAny`
- `action.SagaAny`
- `action.RoundRobinAny`, `HashRouterAny`
- `action.LoopAny`
- `action.AssertAny`
- `action.CatchAny`
- `action.RaceAny`
- `action.TapAny`

Resilience primitives: `action.NewCoalescer`, `action.MemoryIdempotencyStore`,
`action.NewHistory`, `action.Admission` middleware, `action.LoadShedConfig`.

### `kernel/stream` — lazy operators

| API | Use |
|---|---|
| `stream.Map`, `MapE` | Transform each item |
| `stream.Filter` | Keep items matching a predicate |
| `stream.Take` | Bound the stream |
| `stream.Batch` | Group items into fixed-size slices |
| `stream.Collect` | Materialize to a slice (bounded) |
| `stream.Reduce` | Fold |
| `stream.FlatMap` | Expand each item into a sequence |
| `stream.WithContext` | Bind processing to a context |
| `stream.Window`, `Throttle`, `Debounce`, `BatchByTime` | Time-based operators |

Do not replace these with channels or bespoke pipelines without a
measured Kernel gap.

### `kernel/observe` — lifecycle events

| API | Use |
|---|---|
| `observe.Hook(sink)` | Convert a sink into action hooks |
| `observe.Sink` | Neutral event destination |
| `observe.NewSlogSink(logger)` | Structured log sink |
| `observe.NewMemorySink(cap)` | Test / inspection sink |
| `observe.NewMetricsSink()` | In-process counters |
| `observe.NewPrometheusSink()` | Prometheus text output |
| `observe.NewJSONLSink(w, maxBytes)` | Bounded JSONL writer |
| `observe.Event` | Event payload (`Kind`, `Action`, `Duration`, `Request`, ...) |

Event kinds: `KindSuccess`, `KindError`, `KindTimeout`, `KindRetry`,
`KindCacheHit`, `KindCacheMiss`, `KindCanceled`, `KindPanic`,
`KindCoalesced`, `KindDeduplicated`.

### `kernel/xctx` — typed context

| API | Use |
|---|---|
| `xctx.NewKey[T](name)` | Declare a typed context key |
| `xctx.Key.With` / `From` / `MustFrom` | Store and read |
| `xctx.NewScope(parent)` | Pooled request scope + cleanup |
| `xctx.ScopeFrom(ctx)` | Read the active scope |
| `xctx.CloneForAsync(ctx)` | Detach for background work |
| `xctx.WithRequestID` / `RequestIDFrom` | Request identity |
| `xctx.WithExecutionID` / `ExecutionIDFrom` | Execution identity |
| `xctx.WithTraceID` / `SpanIDFrom` etc. | Trace identity |
| `xctx.WithTenantID`, `WithUserID` | Identity |
| `xctx.WithRoles`, `WithPermissions`, `WithFeatures` | Authorization |
| `xctx.WithApprovalToken`, `ApprovalTokenFrom` | HITL approval |
| `xctx.HasRole`, `HasAnyRole`, `HasPermission`, `HasFeature` | Checks |

Never use string context keys.

### `kernel/xerr` — categorized errors

Constructors: `xerr.BadRequest`, `Unauthorized`, `Forbidden`,
`NotFound`, `Conflict`, `Validation`, `TooManyRequests`, `Timeout`,
`Unavailable`, `Internal`, `Canceled`, `RateLimit`, `CircuitBreaker`,
`Database`, `Shutdown`.

Classifiers: `xerr.IsTransient(err)`, `xerr.IsPermanent(err)`,
`xerr.KindFrom(err)`, `xerr.From(err)`, `xerr.PanicRecovery(r)`.

Presentation: `xerr.Sprint(err)`, `xerr.Print(err)`, `(*AppError).Public(requestID)`.

### `kernel/xfs` — filesystem safety

| API | Use |
|---|---|
| `xfs.Rel(p)` | Validate an untrusted relative path |
| `xfs.OpenRoot(dir)` | Open a confined filesystem root |
| `xfs.WriteFileAtomic`, `xfs.WriteFile` | Non-rooted variants (trusted path) |
| `xfs.Root.Open`, `ReadFile`, `Stat`, `Lstat` | Rooted reads |
| `xfs.Root.Create`, `WriteFile`, `WriteFileAtomic` | Rooted writes |
| `xfs.Root.Mkdir`, `MkdirAll`, `Rename`, `Remove`, `RemoveAll` | Rooted mutation |

Rule: any path derived from user input, HTTP, config, or a plugin
manifest goes through `Root`. Reject absolute paths, `..`, and
symlinks that escape the root.

### `kernel/ai/dag` — compiled DAGs

| API | Use |
|---|---|
| `dag.New(name)` | Start a builder |
| `dag.Builder.AddNode`, `AddEdge`, `Compile` | Build the graph |
| `dag.DAG.Execute`, `AsAction`, `ToMermaid` | Run, embed, visualize |
| `dag.AcquireState`, `State.Release` | Pooled state |
| `dag.ReadState.Get`, `Data`, `Clone` | Read |
| `dag.GetNodeOutput[T]` | Typed node output |
| `dag.Suspend(reason, payload)`, `dag.ErrSuspended` | HITL pause |

Do not write another topological sorter in Flow.

### `kernel/xtest` and `kernel/xtest/ktest` — tests

Use `xtest` and `ktest` throughout. Never `testify`, never ad-hoc
polling loops, never `time.Sleep` for synchronization.

`xtest`: `RequireNoError`, `RequireEqual`, `RequireErrorIs`,
`RequireErrorContains`, `RequireErrorKind`, `RequireCondition`,
`RequireStringContains`, `Eventually`, `Never`, `WaitForSignal`,
`WaitForValue`, `RunParallel`, `NewGate`, `NewLatch`, `AllocsPerRun`,
`RequireZeroAlloc`, `RequireMaxAlloc`, `ExpectFatal`, `RequireNoGoroutineLeak`.

`ktest`: `Run`, `RequireEqual`, `RequireErrorKind`, `Recorder`,
`Script`, `Simulate`, `Fake*` builders, `RequestContext`, `Ctx`,
`CtxWithAuth`.

---

## Flow

This repository. Packages are top-level; there is no `compiler/`
wrapper.

### `flow/core` — compiler internals

| API | Use |
|---|---|
| `core.Bundle` | The extension contract — see `docs/extensions.md` |
| `core.Register(id, factory)` | Register a bundle at `init()` |
| `core.CompileAction(...)` | Lower a source string to a runnable action |
| `core.CompileReq` / `core.CompileRes` | Compile request/result |
| `core.Build(ctx, resolver, table, expr)` | Lower an AST fragment |
| `core.Preprocess(ctx, dt, src, name)` | Run directives, return `(clean, meta, err)` |
| `core.NewParserWithFileOffset(ctx, ops, primaries, src, file, lineBase)` | Parse with position tracking |
| `core.Directive` / `DirectiveTable` | Directive contract and registry |
| `core.Modifier` / `ModifierTable` | Modifier contract and registry |
| `core.Operator` / `OperatorTable` | Compiler-level operators (`->`, `&`, `||`) |
| `core.PrimaryExtension` | Parser extension contract (`TokenPrimary`, `KeywordPrimary`) |
| `core.CapabilityResolver` | Runtime capability lookup (`Action`, `Stream`, `Operator`) |
| `core.DynamicResolver` | Default resolver; supports `Mount`, `MountWithAlias` |
| `core.CapabilityRef` / `ParseCapabilityRef` | Bare-identifier capability references |
| `core.ArgFieldSpec` / `ArgKind` | Argument schemas for capability arguments |
| `core.LineLookup` / `ModifierSource` | Line-indexed modifier chain and its provenance |
| `core.WithLineModifiers(...)` | Compile option that installs the chain |
| `core.LineModifiersFromOptions(mt, opts)` | Extract the chain from compile options |
| `core.ModifierName(raw)` | Name portion of a raw modifier |
| `core.SourceError(pos, ...)` | Compile-time error with a position |
| `core.BuildCatalog(...)` | Dump the compiler surface |
| `core.KeywordMappings()` / `TranslateKeyword` | Native keyword grammar |
| `core.IsReservedActionName`, `RequiredModifierOwner` | Reserved-name policy |

### `flow/runner` — execution assembly

| API | Use |
|---|---|
| `runner.BuildConfig(bundles)` | Build a `Config` from bundles |
| `runner.Config` | The execution configuration passed to `Execute` |
| `runner.Execute(ctx, cfg, src, name, payload)` | Compile and run |
| `runner.Execution` | Result: `Output`, `Meta`, `Resolver`, timings, allocs |
| `runner.RunSource(ctx, cfg, src, name, payload, opts)` | CLI-oriented wrapper with `--info` and observer |
| `runner.SplitPipelineModifiers(table, mods)` | Split wrapper vs. body modifiers |
| `runner.SplitPipelineModifiersWithSources(table, mods, srcs)` | Same, sources preserved |
| `runner.NewObserver(w, verbosity)` | Live per-action observer |
| `runner.PrintInfo` | Render a pipeline shape |
| `runner.RunAssertions` | Evaluate `@assert:` directives |

### `flow/native` — built-in bundles

```go
native.Bundles()      // all standard extensions as []core.Bundle
native.Directives()   // the standard DirectiveTable
native.Primaries()    // the standard primary extensions
```

`native.Bundles()` is the single source of truth for what the shipped
CLI has available. Add a new standard bundle there.

### `flow/extensions/` — standard bundles

| Bundle | Contributes |
|---|---|
| `assert` | `@assert:` directive, `assert(cond, msg)` keyword |
| `config` | `@config`, `@config.load` (JSON, TOML, registry-based YAML) |
| `config_yaml` | Opt-in YAML loader for `@config.load` (`@require config_yaml`) |
| `decide` | `decide.run` action (pluggable decision backends) |
| `description` | `@description` |
| `external` | `external.exec`, `http.request`, `external.wasm` |
| `fs` | `fs.walk` source; `fs.filter`, `fs.read`, `fs.sort`, `fs.write`, `out.stdout`, `out.file` |
| `hook` | `@hook:name` directive; `hook.probe` action |
| `include` | `@include` |
| `loop` | `loop(...) until(...)` keyword |
| `macros` | `@macro` engine |
| `match` | `match(subject) { ... }` keyword |
| `modifiers_auth` | `:auth`, `:role=`, `:perm=`, `:feature=` |
| `modifiers_core` | `:timeout=`, `:retry=`, `:cache=`, `:dedup`, `:coalesce`, `:rate_limit=`, `:concurrency=`, `:idempotent` |
| `modifiers_meta` | `:name=`, `:desc=`, `:status=`, `:tag=`, `:scope=`, `:strict`, `:lenient`, ... |
| `nodes_bench` | `bench.run`, `bench.save`, `bench.compare` |
| `nodes_dispatch` | `dispatch.run` |
| `nodes_distribute` | `distribute.map`, `distribute.reduce` |
| `nodes_log` | `log.info`, `log.warn`, `log.error` |
| `nodes_supervisor` | `supervisor.run` |
| `on` | `@on event "protocol:target"` |
| `on_error` | `@on_error { when ... }` block |
| `pipeline` | `@pipeline` directive and materializer |
| `pool` | `@pool NAME [...]` directive; materializes a callable `pool.NAME` action |
| `projection` | `{ ... }` projection syntax |
| `render` | `render.markdown` operator |
| `require` | `@require` directive |
| `retry` | `AtomAdvise` for `:retry=N` |
| `runtime` | The primitive action library (`runtime.const`, `runtime.noop`, ...) |
| `schema` | `@schema NAME { ... }` + `schema.validate` |
| `scope` | `@scope` and `@profile` directives |
| `selftestkit` | Coverage actions (`cov.echo`, `cov.flaky`, ...) |
| `syntax` | `->`, `|`, `&`, `||` operators |
| `template` | `template.render` |

Look here first when adding a DSL feature.

### `flow/cli` — the CLI

| API | Use |
|---|---|
| `cli.Run(args)` | Top-level dispatcher |
| `cli.RunWithBundles(args, bundles)` | Entry point for harness binaries |
| `cli.RunEmbedded(ctx, src, args)` | Entry point for `nflow build` binaries |
| `cli.SourceRequires(path)` | Extract `@require` declarations from a file |
| `cli.EnsureHarness`, `cli.ExecHarness` | Harness cache and invocation |
| `cli.Completion(shell, w)` | Shell completion scripts |
| `cli.Print`, `cli.Version` | Build metadata |
| `cli.SelfBuild(ctx)` | In-place rebuild |

### `flow/contracts` — context bridges

| API | Use |
|---|---|
| `contracts.WithPools` / `PoolsFromContext` | Optional named-pool context bridge for integrations; `dispatch.run` does not use it |
| `contracts.WithRecoveredError` / `RecoveredErrorFrom` | Error recovery handoff |
| `contracts.WithCompiler` / `CompilerFromContext` | Compiler handoff (used by `supervisor`) |
| `contracts.WithActionResolver` / `ActionResolverFromContext` | Resolver handoff |

### `flow/template` — text template rendering

Package-agnostic. `flowtemplate.Render(source, vars)` — Go `text/template`
syntax, missing-key error, no HTML escaping, no shell.

---

## Naming policy

One canonical, qualified name per capability:

```
runtime.const      fs.read       http.request
match.evaluate     pipeline.fetch    pool.workers
distribute.map     dispatch.run      supervisor.run
```

**No new short aliases.** `action.Library.Aliases` is rejected at mount
time. The one alias mechanism Flow supports is `@require ... as NAME`,
which is per-compilation and does not mutate the source library.

Native keywords (`const`, `noop`, `pick`, ...) are grammar, not
aliases. They live in `core/reserved.go` and are not extensible.

---

## Update policy

This file is **curated, not generated**. When a public API changes:

1. Update the implementation and tests.
2. Update this file in the same change.
3. Run `go test ./...` and `nflow self test`.
4. Remove an entry only after verifying no supported code uses it.

If a section here disagrees with the source, the source wins and this
file is wrong. Fix it in the same commit.
