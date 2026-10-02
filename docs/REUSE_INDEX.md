# Nexss Reuse Index

**Purpose:** Fast navigation for contributors and agents before adding code.

This is a **navigation index**, not a replacement for reading the actual contract. Verify signatures against the checked-out source and the pinned dependency version before use. Prefer existing Kernel and Flow primitives over local helpers.

## Priority rule

1. Search `github.com/nexssp/kernel` first for domainless runtime behavior.
2. Search this Flow repository for compiler, runner, registry, transport, and DSL behavior.
3. Search the relevant Nexss domain package (`cost`, `validation`, `transport`, `ai`, etc.).
4. Add new code only when no existing contract fits.

Do not copy an existing primitive into an extension, node, runner, or transport.

---

## Kernel: actions and registries

Package: `github.com/nexssp/kernel/action`

| API | Use | Do not reimplement |
|---|---|---|
| `action.New[Req, Res]` | Build a typed action | Local action wrappers with duplicate lifecycle behavior |
| `action.NewStream[Req, Item]` | Build a typed stream source | Custom source goroutines |
| `action.NewStreamOp[Req, In, Out]` | Attach a typed stream operator | Local stream plumbing |
| `action.NewOperator[In, Out, Cfg]` | Declare a typed runtime stream operator | Reflection-based operator registries |
| `action.NewSimpleOperator[In, Out]` | Declare a simple operator without custom config | Repeated trivial operator builders |
| `action.Dynamic` | Bridge a typed action to `any` | Unsafe ad-hoc type erasure |
| `action.InvokeAny` | Invoke an action through the erased boundary | Direct type switches over actions |
| `action.NewProxy` | Decorate/compose an action | Custom proxy wrappers |
| `action.NewStreamProxy` | Decorate/compose a stream | Custom stream proxy wrappers |
| `action.NewTypedStreamProxy` | Typed stream proxy composition | Repeated typed proxy code |
| `action.NewRegistry` | Register Kernel libraries | A second action registry |
| `action.Registry.Get` / `GetStream` / `GetOperator` | Resolve registered capabilities | Direct access to registry internals |
| `action.Registry.Names` | Enumerate registered capabilities | Unstable map iteration |
| `action.Library` | Package actions, streams, operators, hooks, aliases | Flow-specific library structures |
| `action.CloneWithHooks` | Attach execution hooks safely | Mutating shared action instances |

## Kernel: composition and resilience

Use the current Kernel action composition/resilience APIs before writing Flow-specific equivalents. Confirm exact names in the pinned Kernel source before use.

- action pipe/composition helpers;
- parallel and fan-out helpers;
- retry and timeout middleware;
- cache, idempotency, coalescing, and circuit-breaker middleware;
- action hooks and lifecycle callbacks;
- stream collection boundaries.

**Rule:** Flow describes and assembles these capabilities; Kernel executes them.

## Kernel: streams

Package: `github.com/nexssp/kernel/stream`

| API | Use |
|---|---|
| `stream.Map` | Transform every item |
| `stream.MapE` | Transform every item with an error result |
| `stream.Filter` | Keep items matching a predicate |
| `stream.Take` | Bound the number of items |
| `stream.Batch` | Group items into fixed-size batches |
| `stream.Collect` | Materialize a bounded stream into a slice |
| `stream.Reduce` | Fold items into an accumulator |
| `stream.FlatMap` | Expand each item into another sequence |
| `stream.WithContext` | Bind stream processing to a context |

These are lazy typed operators. Do not replace them with channels, slice-shifting loops, or custom goroutine pipelines without a measured Kernel gap.

## Kernel: observability

Package: `github.com/nexssp/kernel/observe`

| API | Use |
|---|---|
| `observe.Hook` | Convert a sink into action lifecycle hooks |
| `observe.Sink` | Neutral event destination contract |
| `observe.NewMemorySink` | Test/event inspection sink |
| `observe.NewMetricsSink` | In-process metrics aggregation |
| `observe.NewSlogSink` | Structured log sink |
| `observe.NewPrometheusSink` | Prometheus-compatible metrics sink |
| `observe.NewJSONLSink` | Bounded JSONL event output |
| `observe.Event` | Lifecycle event payload |

Do not add domain-specific fields to Kernel lifecycle events for one Flow extension. Add a typed domain trace beside the neutral sink when necessary.

## Kernel: context and scope

Package: `github.com/nexssp/kernel/xctx`

| API | Use |
|---|---|
| `xctx.NewKey[T]` | Declare a typed context key |
| `xctx.Key.With` / `From` / `MustFrom` | Store and read typed context values |
| `xctx.NewScope` | Create a request scope |
| `xctx.ScopeFrom` | Read the active request scope |
| `xctx.CloneForAsync` | Safely detach/copy scope for async work |
| `xctx.WithRequestID` / `RequestIDFrom` | Request identity |
| `xctx.WithExecutionID` / `ExecutionIDFrom` | Execution identity |
| `xctx.WithTraceID` / `TraceIDFrom` | Trace identity |
| `xctx.WithSpanID` / `SpanIDFrom` | Span identity |
| `xctx.WithTenantID` / `TenantIDFrom` | Tenant identity |
| `xctx.WithUserID` / `UserIDFrom` | User identity |
| `xctx.WithRoles` / `RolesFrom` | Authorization roles |
| `xctx.WithPermissions` / `PermissionsFrom` | Authorization permissions |

Never use string context keys or store request-scoped mutable state in package globals.

## Kernel: errors

Package: `github.com/nexssp/kernel/xerr`

| API | Use |
|---|---|
| `xerr.BadRequest` | Invalid request/input |
| `xerr.Unauthorized` / `Forbidden` | Authentication/authorization violations |
| `xerr.NotFound` | Missing resource |
| `xerr.Conflict` | Duplicate or conflicting state |
| `xerr.Unavailable` | Transient upstream failure |
| `xerr.Timeout` | Deadline exceeded |
| `xerr.RateLimit` | Rate limiting |
| `xerr.CircuitBreaker` | Circuit state prevents execution |
| `xerr.Canceled` | Explicit cancellation |
| `xerr.Internal` | Unexpected implementation failure |
| `xerr.From` | Normalize an error to `*xerr.AppError` |
| `xerr.IsTransient` / `IsPermanent` | Retry classification |
| `xerr.KindFrom` | Read the error kind |
| `xerr.Sprint` / `Print` | Safe error presentation |

Do not leak bare `errors.New` or unclassified `fmt.Errorf` across public package boundaries.

## Kernel: filesystem

Package: `github.com/nexssp/kernel/xfs`

| API | Use |
|---|---|
| `xfs.Rel` | Validate a relative untrusted path |
| `xfs.OpenRoot` | Open a contained filesystem root |
| `xfs.WriteFile` | Write a file through the safe filesystem helper |
| `xfs.WriteFileAtomic` | Durable atomic write |
| `xfs.Root.Open` / `ReadFile` / `Stat` / `Lstat` | Rooted reads and metadata |
| `xfs.Root.Create` / `WriteFile` | Rooted writes |
| `xfs.Root.WriteFileAtomic` | Rooted durable atomic write |
| `xfs.Root.Mkdir` / `MkdirAll` | Rooted directory creation |
| `xfs.Root.Rename` / `Remove` / `RemoveAll` | Rooted mutation |

Never concatenate user-controlled paths. Reject absolute paths, traversal, and escaping symlinks.

## Kernel: AI DAG

Package: `github.com/nexssp/kernel/ai/dag`

| API | Use |
|---|---|
| `dag.New` | Start a typed DAG builder |
| `dag.Builder.AddNode` | Add an action node |
| `dag.Builder.AddEdge` | Add a dependency edge |
| `dag.Builder.Compile` | Validate and compile the graph |
| `dag.DAG.Execute` | Execute a compiled DAG |
| `dag.DAG.AsAction` | Expose a DAG as a Kernel action |
| `dag.AcquireState` / `State.Release` | Pool DAG state safely |
| `dag.ReadState.Get` / `Data` / `Clone` | Read isolated state |
| `dag.State.Get` / `Set` / `Clone` | Mutate owned state |
| `dag.GetNodeOutput[T]` | Typed node-output access |
| `dag.DAG.ToMermaid` | Deterministic graph visualization |

Do not write another topological sorter or graph executor in Flow.

## Kernel: testing

Package: `github.com/nexssp/kernel/xtest` and `github.com/nexssp/kernel/xtest/ktest`

Use the existing assertion, eventual, parallel, latch, trace, and simulation helpers. Do not add `testify`, ad-hoc polling loops, or `time.Sleep` synchronization.

---

## Flow: compiler and parsing

Package paths are relative to this repository.

| API | Use |
|---|---|
| `compiler.NewCompiler` | Construct the current Flow compiler |
| `compiler/core.CompileAction` | Compile source into an action |
| `compiler/core.RunAction` | Run compiled Flow source |
| `compiler/core.CompileAndRun` | Compile and execute in one action |
| `compiler/core.Build` | Lower an AST expression through the current registry |
| `compiler.NewLexer` | Lex source |
| `compiler.NewParser` / `NewParserWithFile` | Parse source |
| `compiler/core.NewDirectiveTable` | Build directive registry |
| `compiler/core.NewModifierTable` | Build modifier registry |
| `compiler/core.NewOperatorTable` | Build compiler-operator registry |
| `compiler/core.NewPrimaryTable` | Build primary-extension registry |

Do not add compiler grammar or lowering logic to nodes or transports.

## Flow: registry and actions

Package: `github.com/nexssp/flow`

| API | Use |
|---|---|
| `flow.NewRegistry` | Create the Flow capability registry |
| `flow.Registry.Register` | Register actions, sources, and runtime operators |
| `flow.Registry.Get` | Resolve an action |
| `flow.Registry.GetStream` | Resolve a stream source |
| `flow.Registry.GetOperator` | Resolve a stream operator |
| `flow.Registry.Resolve` | Determine the registered capability kind |
| `flow.Registry.Names` | Enumerate registered names |
| `flow.RegistryFromActionRegistry` | Bridge a Kernel registry to Flow |
| `flow.CompilePipeline` | Compile a pipeline expression using a Kernel registry |
| `flow.CompileSaga` | Compile a saga expression |
| `flow.RegisterPipelines` | Register named Flow pipelines |
| `flow.NewTypedStreamOperator` | Adapt a typed stream operator |

Duplicate names must fail. Do not depend on short aliases; use canonical qualified names.

## Flow: nodes and reusable adapters

Package: `github.com/nexssp/flow/nodes`

| API | Use |
|---|---|
| `nodes.NewNoopAction` | Identity/no-op action |
| `nodes.NewConstAction` | Constant result action |
| `nodes.NewPickAction` | Select a field |
| `nodes.NewWrapAction` | Wrap a value |
| `nodes.NewFailAction` | Explicit failure action |
| `nodes.NewLogInfoAction` / `NewLogWarnAction` / `NewLogErrorAction` | Structured logging actions |
| `nodes.NewProjectionAction` | Projection action |
| `nodes.NewLoopAction` | Flow loop adapter |
| `nodes.NewDispatchAction` | Dispatch action |
| `nodes.NewDistributeMapAction` / `NewDistributeReduceAction` | Distribution actions |
| `nodes.NewPromptNode` | Prompt/domain adapter; use only when the AI contract is intended |
| `nodes.NewAssertAction` | Runtime assertion action |

These are Flow adapters. If the behavior is domainless or resilience-related, prefer Kernel directly.

## Flow: runner and tests

| API | Use |
|---|---|
| `runner.RunFlow` | Execute a Flow request through the standard runner |
| `runner.RunFlowTest` | Execute a Flow test command |
| `runner.NewRunnerObserver` | Standard runner metrics/trace observer |
| `runner.NewRunnerObserverWithHooks` | Runner observer with lifecycle hooks |
| `runner.NewMemoryCheckpointStore` | In-memory checkpoint store |
| `runner.NewFileCheckpointStore` | File-backed checkpoint store |
| `flow/testkit.New` | Build a Flow test harness |
| `flow.NewWorkflowTest` | Build a workflow test fixture |
| `runner/testkit.RunDSL` | Run DSL through the runner test harness |

## Flow: transport and external capabilities

| API | Use |
|---|---|
| `transport.Register` | Register a transport factory by prefix |
| `transport.Resolve` | Resolve a transport binding |
| `transport.OnDSL` / `transport.OnTrigger` | Declare transport binding points |
| `runner/capability.NewResolver` | Resolve HTTP, exec, and WASM capability bindings |
| `flow.RegisterMaterializer` | Register a declared materializer |
| `flow.RegisterBoundary` | Register a stream/materialization boundary |

Concrete NATS, Redis, PostgreSQL, HTTP, and provider behavior belongs in its own package or extension. Do not add transport-specific logic to the compiler.

---

## Naming and alias policy

Use one canonical name per capability, normally qualified by domain:

```text
fs.read
ai.route
transport.request
sandbox.run
cost.record
```

Do not introduce new short aliases such as `read`, `route`, or `invoke`. They create collisions, make search incomplete, and hide which package owns the capability. `@require ... as local` is a file-local namespace choice and is acceptable when it is explicit.

## Update policy

This file is a curated index, not generated truth. When a public API changes:

1. update the implementation and tests;
2. update this index in the same change;
3. run the full validation suite;
4. remove entries only after verifying no supported code uses them.

The index should stay short enough to scan. For complete signatures, read the source and package documentation.
