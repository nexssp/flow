# Nexss Flow / nexssp/flow

Action orchestration engine, Arrow DSL, and deterministic DAG compiler
for Go.

`flow` compiles a compact text pipeline into a typed, executable graph.
Every node in the graph is a `nexssp/kernel` action — a typed Go
function, a remote call, a subprocess, a WebAssembly module, or another
pipeline. Flow provides the DSL, the compiler, the runtime, the built-in
nodes, and the extension points that let you add your own.

- Module: `github.com/nexssp/flow`
- Go: 1.26
- Built with nexss open source packages: `kernel`, `cost`, `transport`, `transportai`, `validation`, `testkit`

---

## Contents

1. [Install](#install)
2. [Quick start](#quick-start)
3. [The .flow file format](#the-flow-file-format)
4. [Arrow DSL operators](#arrow-dsl-operators)
5. [Directives](#directives)
6. [Action modifiers](#action-modifiers)
7. [Transports](#transports)
8. [Configuration](#configuration)
9. [Built-in nodes](#built-in-nodes)
10. [Library system](#library-system)
11. [Running flows](#running-flows)
12. [Cost governance](#cost-governance)
13. [Checkpointing and resume](#checkpointing-and-resume)
14. [Approval and HITL](#approval-and-hitl)
15. [Journaling and replay](#journaling-and-replay)
16. [Snapshots](#snapshots)
17. [Observability](#observability)
18. [Telemetry hot path](#telemetry-hot-path)
19. [State and conditions](#state-and-conditions)
20. [YAML graph definition](#yaml-graph-definition)
21. [Learning router](#learning-router)
22. [Evolutionary optimizer](#evolutionary-optimizer)
23. [Extending Flow](#extending-flow)

---

## Install

```bash
go get github.com/nexssp/flow@latest
```

For the CLI:

```bash
go install github.com/nexssp/flow/cmd/nexssflow@latest
```

---

## Quick start

```go
package main

import (
    "context"
    "fmt"

    "github.com/nexssp/flow"
    "github.com/nexssp/kernel/action"
)

func main() {
    ctx := context.Background()

    fetch := action.New("user.fetch", func(_ context.Context, id int) (map[string]any, error) {
        return map[string]any{"id": id, "name": "Alice", "tier": "pro"}, nil
    }).Build()

    notify := action.New("email.send", func(_ context.Context, req map[string]any) (string, error) {
        return fmt.Sprintf("sent to %v", req["to"]), nil
    }).Build()

    reg, err := action.NewRegistry(action.Of(fetch, notify))
    if err != nil { t.Fatal(err) }

    pipeline, err := flow.CompilePipeline(
        `user.fetch -> { to: .name, subject: "Welcome " + .tier } -> email.send`,
        reg,
    )
    if err != nil {
        panic(err)
    }

    res, err := pipeline.Build().Do(ctx, 42)
    if err != nil {
        panic(err)
    }

    fmt.Println(res) // sent to Alice
}
```

---

## The .flow file format

A `.flow` file is a plain text pipeline. It may contain directives
(lines starting with `@`), a route declaration header, and the pipeline
body.

```elixir
# pipeline comments start with # or //
@config:budget_usd=1.00
@config:approval=danger
@assert: success == true

users.solve:route="POST /api/users/solve":status=200
  users.validate
  -> users.enrich
  -> ( users.audit & users.notify )
  -> users.finalize
```

The route declaration header names the flow and binds its HTTP route. It
is not part of the pipeline; the sanitizer removes it before the
pipeline is compiled.

---

## Arrow DSL operators

| Operator | Syntax | Meaning |
|---|---|---|
| Sequential pipe | `A -> B` or `A \| B` | Passes A's output to B |
| Parallel scatter | `( A & B & C )` | Runs concurrently, gathers into `map[string]any` |
| Fallback chain | `A \|\| B` | Tries A; on error runs B (`FirstSuccess`) |
| Conditional | `gate ? target` | Runs target only when gate's output is truthy |
| Autonomous loop | `loop( A ) until( cond )` | Repeats until cond is true, bounded at 15 turns |
| Inline projection | `{ key: expr }` | Reshapes the payload between nodes (expr-lang) |

Operator precedence (tightest to loosest):

```
primary  { }  ( )  atom  loop ... until ...
&        parallel
-> |     pipe
||       fallback
?        conditional
```

### Worked examples

```flow
# RAG pipeline
vector.search
-> { context: .results, question: .user_query }
-> llm.generate_answer
-> ui.render
```

```flow
# CI/CD with conditional routing
git.pull
-> make.build
-> go.test
-> ( state.tests_passed == true ? k8s.deploy : slack.alert_failure )
```

```flow
# Cheap fallback chain
( redis.get || postgres.query || legacy.rest_api )
-> { formatted_data: .raw_json }
-> http.respond
```

```flow
# Parallel fan-out with fan-in projection
stripe.charge
-> ( pdf.generate_invoice & warehouse.trigger_robot )
-> { status: "processing", invoice_url: .pdf.generate_invoice.url }
-> email.send_receipt
```

```flow
# Bounded autonomous loop
{ attempts: 0, message: "turn 1" }
-> loop(
    { attempts: attempts + 1, message: "turn " + string(attempts + 1) }
    -> log.info
) until( attempts >= 3 )
```

---

## Directives

Directives start a line with `@`. They are processed at preprocess time
and do not appear in the compiled pipeline.

| Directive | Purpose |
|---|---|
| `@config:key=value` | Override a runtime knob |
| `@assert: expr` | Testkit assertion evaluated after the flow finishes |
| `@pipeline Name` … `@end` | Declare a named subflow, callable by name |
| `@include ./path.flow` | Merge pipelines and requires from another file |
| `@action name` | Expose this file as a callable action named `name` |
| `@description "text"` | Human-readable description for the action |
| `@require ./local/path` | Import a local Go library |
| `@require module vX.Y.Z` | Import a published Go library |

`@include` is transitive and cycle-safe. `@require` local paths resolve
to the containing module path by walking up to the nearest `go.mod`.

### Named pipelines

```flow
@pipeline greet
  { message: "hello, " + name }
  -> log.info
@end

{ name: "world" } -> greet
{ name: "again" } -> greet
```

A pipeline is registered as an action under its name and can be called
from anywhere in the same manifest or from any file that `@include`s it.

### Requiring libraries

```flow
@require ./text
@require github.com/acme/text-tools v1.0.0

{ message: "hello, world" }
-> text_tools.uppercase
-> log.info
```

Each required package must export `func Library() flow.Library`.

---

## Action modifiers

Modifiers are `:key=value` pairs appended to an atom. They configure the
action at call time. Order is not significant. Boolean flags have no
`=value`:

```flow
agent.critic:model="deepseek-flash":timeout=10s:retry=2:cache=5m
-> users.save:validate:idempotent:status=201
-> tools.search:coalesce:dedup
```

### Routing and identity

| Modifier | Effect |
|---|---|
| `:route="METHOD /path"` | Bind an HTTP route |
| `:http="METHOD /path"` | Alias for `:route=` |
| `:name=identifier` | Rename the action |
| `:desc="text"` / `:description="text"` | Description override |
| `:type=Req->Res` | Override request/response type names |
| `:scope=public\|internal\|system` | Action scope |
| `:status=code` | HTTP success status |

### Resilience

| Modifier | Effect |
|---|---|
| `:timeout=30s` | Per-call timeout |
| `:retry=N` | Max retry attempts |
| `:retry_if=predicate` | Retry predicate (default: transient only) |
| `:backoff=strategy,base:X,max:Y` | Backoff strategy |
| `:backoff_base=100ms` | Backoff base (explicit form) |
| `:backoff_max=30s` | Backoff ceiling (explicit form) |
| `:breaker=failures:N,cooldown:30s` | Circuit breaker |
| `:breaker_failures=N` | Failure threshold |
| `:breaker_cooldown=30s` | Half-open reset delay |
| `:priority=critical\|normal\|low` | Load-shedding tier |
| `:concurrency=N` | Max concurrent in-flight calls |
| `:rate_limit=N/s` | Token bucket rate limit |
| `:burst=N` | Rate-limit burst size |

### Caching and deduplication

| Modifier | Effect |
|---|---|
| `:cache=5m` | Read-through cache TTL |
| `:cache_key=...` | Custom cache key |
| `:coalesce` | Share in-flight results across concurrent callers |
| `:dedup` | Same-key callers block until the first completes |
| `:idempotent` | Register idempotency metadata |
| `:idempotency_header=X-Key` | Custom idempotency header name |

### Security and governance

| Modifier | Effect |
|---|---|
| `:auth` | Require an authenticated context |
| `:role=name` | Require a role |
| `:perm=name` | Require a permission |
| `:feature=flag` | Require a feature toggle |
| `:budget_micros=N` | Per-node cost estimate for the reservation guard |
| `:budget=$1.00` | Same, in currency units |
| `:audit` | Emit an audit record on success |
| `:hitl="prompt"` | Mark for human-in-the-loop approval |
| `:hitl_options=a,b,c` | Approval option labels |
| `:hitl_trigger=reason` | Trigger condition description |

### Lifecycle and deprecation

| Modifier | Effect |
|---|---|
| `:deprecated` | Mark deprecated |
| `:since=v1.2.0` | Deprecation version |
| `:use=replacement` | Suggested replacement |
| `:validate` | Enable request struct validation |
| `:debug` | Log every invocation |

### Transport-specific

| Modifier | Effect |
|---|---|
| `:channel=name` | SSE channel |
| `:cli_alias=a,b` | CLI command aliases |
| `:cli_desc="text"` | CLI help text |
| `:a2a_desc="text"` | A2A role description |
| `:a2a_example=text` | A2A usage example |

---

## Transports

Every transport modifier accepts a target. Multiple transports can be
attached to the same action.

| Modifier | Target format | Purpose |
|---|---|---|
| `:route=` | `"METHOD /path"` | HTTP REST |
| `:sse=` | `"/path"` | Server-Sent Events |
| `:raw=` | `"METHOD /path"` | Raw HTTP handler |
| `:cli=` | `"command:help"` | CLI subcommand |
| `:cron=` | `"every 5m"` or `"* * * * *"` | Cron schedule |
| `:worker=` | `"every 30s"` | Background worker |
| `:topic=` | `"topic.name"` | In-process bus |
| `:a2a=` | `"role"` | Agent-to-agent |
| `:nats=` | `"subject"` | NATS pub/sub |
| `:nats_rpc=` | `"subject"` | NATS request/reply |
| `:nats_kv=` | `"bucket/key"` | NATS KV get/watch |
| `:nats_durable=` | `"stream/subject/durable[/dlq]"` | JetStream durable work |
| `:nats_consumer=` | `"stream/subject/durable"` | Custom JetStream consumer |
| `:nats_obj=` | `"bucket/pattern"` | NATS Object Store |
| `:nats_svc=` | `"service/version/endpoint/subject"` | NATS microservice |

### Capability bindings

A `.flow` file can proxy a node to an external target without writing Go:

| Modifier | Target | Behaviour |
|---|---|---|
| `:remote="http://..."` | URL | JSON POST request/response |
| `:exec="rg --json"` | shell command | JSON on stdin, JSON on stdout |
| `:wasm="./x.wasm"` | wasm file | JSON on stdin, JSON on stdout (wazero) |

The first call compiles the WASM module; subsequent calls reuse it.

```flow
agent.worker:remote="http://10.0.0.6:9002/work":timeout=45s
tools.ripgrep:exec="rg --json":timeout=10s
skills.lint:wasm="./skills/lint.wasm":timeout=15s
```

---

## Configuration

Four layers, applied in this order (later overrides earlier):

```
CLI  >  env  >  @config:  >  defaults
```

### Knobs

| Knob | Type | Default | Purpose |
|---|---|---|---|
| `verbosity` / `v` | int | 0 | 0–3, clamped |
| `budget_micros` | int | 10,000,000 | Hard cost ceiling |
| `budget_usd` / `budget` | float | — | Same, in USD |
| `approval` | string | `danger` | `danger`, `all`, `none` |
| `max_tokens` | int | 0 | Per-run LLM token ceiling |
| `observe` | string | `live` | `live`, `json`, `off` |
| `provider` | string | — | Default LLM provider |
| `model` | string | — | Default model |
| `sandbox` | string | — | Sandbox driver |
| `out` / `output_format` | string | — | `json`, `text` |
| `out_dir` / `output_dir` | string | `.runs` | Output directory |

### Environment variables

`NEXSS_VERBOSITY`, `NEXSS_BUDGET_MICROS`, `NEXSS_BUDGET_USD`,
`NEXSS_APPROVAL`, `NEXSS_MAX_TOKENS`, `NEXSS_OBSERVE`, `NEXSS_PROVIDER`,
`NEXSS_MODEL`, `NEXSS_SANDBOX`, `NEXSS_OUT`, `NEXSS_OUT_DIR`.

### CLI flags

```
-v | -vv | -vvv         verbosity
-q | --quiet            silence
--budget=<usd>          budget in USD
--budget-micros=<n>     budget in micros
--max-tokens=<n>        LLM token ceiling
--approval=<mode>       danger | all | none
--observe=<mode>        live | json | off
--provider=<name>
--model=<name>
--sandbox=<name>
--out=<format>
--out-dir=<path>
-i | --info             describe flow, do not execute
--assert="expr"         testkit assertion
--resume=<runID>        resume from checkpoint
--bench=<node>          benchmark a node
--bench-runs=<n>        benchmark iterations
--cache=<dir>           cache directory
```

---

## Built-in nodes

`flow.StandardLibrary()` provides:

| Node | Purpose |
|---|---|
| `log.info` / `log.warn` / `log.error` | Structured log, passes payload through |
| `bench.run` | Run another action N times, report latency distribution |
| `bench.save` | Write a benchmark result to a file |
| `bench.compare` | Compare current benchmark against a baseline |
| `distribute.map` | Invoke an action once per item, bounded concurrency |
| `distribute.reduce` | Fold a `distribute.map` result |
| `supervisor` | Compile and run child pipelines on the fly |

`bench.run`, `distribute.map`, and `supervisor` resolve their target
through the registry the compiler places on the execution context.

### Aliases

`log`, `info`, `warn`, `error`, `bench`, `benchmark`, `compare`, `diff`,
`map`, `fanout`, `parallel`, `reduce`, `fold`.

Canonical names always win over aliases; user-provided actions always win
over built-in aliases.

---

## Library system

A library is a named bag of actions, hooks, and aliases:

```go
type Library struct {
    Name        string
    Description string
    Actions     []action.AnyAction
    Hooks       []action.AnyHook
    Aliases     []Alias           // Alias{Canonical, Short []string}
    Overrides   []string          // canonical names this library intentionally replaces
}
```

`flow.BuildRegistry(libs...)` applies four rules:

1. Every primary action registers under its canonical name.
2. When two libraries declare the same canonical name, the later library
   must list it in `Overrides`, otherwise `BuildRegistry` returns an error.
3. Hooks from every library are applied to every surviving action.
4. Aliases are registered last, so canonical names always win.

```go
reg, err := flow.BuildRegistry(
    flow.StandardLibrary(),
    myLibrary,
)
```

### Declaring your own library

```go
package mylib

import (
    "github.com/nexssp/flow"
    "github.com/nexssp/kernel/action"
)

func Actions() []action.AnyAction {
    return []action.AnyAction{
        action.New("mylib.echo", func(_ context.Context, s string) (string, error) {
            return s, nil
        }).Tag("mylib").Build(),
    }
}

func Library() flow.Library {
    return flow.Library{
        Name:        "mylib",
        Description: "Example library",
        Actions:     Actions(),
        Aliases: []flow.Alias{
            {Canonical: "mylib.echo", Short: []string{"echo"}},
        },
    }
}
```

Consume it in a flow file with `@require ./mylib`, or programmatically:

```go
reg, err := flow.BuildRegistry(
    flow.StandardLibrary(),
    mylib.Library(),
)
```

---

## Running flows

### CLI

```bash
nexssflow ./pipeline.flow '{"user_id": 42}' -vvv --assert="success == true"
nexssflow ./pipeline.flow --info
nexssflow ./pipeline.flow --resume=run_1731000000
```

### Programmatic

```go
req := flowrunner.Request{
    Path:    "./pipeline.flow",
    Payload: map[string]any{"user_id": 42},
    Args:    []string{"-vv"},
    Stdout:  os.Stdout,
    Stderr:  os.Stderr,
}

exit := flowrunner.Default{}.RunFlow(ctx, req.Path, req.Payload, req.Args,
    []flow.Library{flow.StandardLibrary()}, req.Stdout, req.Stderr)
```

Or with a custom registry:

```go
exit := flowrunner.RunWithRegistry(ctx, req, reg, observer)
```

### Embedded compilation

```go
compiler := flow.NewCompiler(reg,
    flow.WithApprovalGate(gate),
    flow.WithReserver(ledger),
    flow.WithJournal(journal),
    flow.WithHooks(obsHook, liveHook),
)

execAct := flow.NewExecuteAction(compiler)

res, err := execAct.Do(ctx, flow.GraphExecReq{
    DSL:            pipelineDSL,
    InitialPayload: payload,
})
```

### Describing a flow

```go
exit := flowrunner.PrintFlowInfo(ctx, os.Stdout, req, reg)
```

Prints the pipeline topography, entry payload shape, and per-node
metadata.

---

## Cost governance

```go
ledger := cost.NewLedger(10_000_000, cost.USD)   // $10.00

compiler := flow.NewCompiler(reg, flow.WithReserver(ledger))
```

Attach to individual actions:

```go
guarded := action.New("ai.complete", handler).
    AnyHook(flow.GuardCost(ledger, 50_000)).   // reserve $0.05
    Build()
```

The compiler reserves the estimated cost of every node before execution.
If the budget is exceeded, the node fails before the handler runs.

The ledger reports per-currency totals; currencies are never summed
against each other.

### Multi-tenant ledgers

```go
type TenantLedgerRegistry struct {
    mu      sync.RWMutex
    ledgers map[string]*cost.Ledger
}

func TenantCostHook(reg *TenantLedgerRegistry, estimate int64) action.AnyHook {
    return action.AnyHook{
        Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
            tenant, _ := TenantFromContext(ctx)
            ledger, ok := reg.Get(tenant.TenantID)
            if !ok {
                return ctx, xerr.Forbidden("tenant has no ledger")
            }
            reservation, err := ledger.Reserve(ctx, estimate)
            if err != nil {
                return ctx, err
            }
            return context.WithValue(ctx, reservationKey{}, reservation), nil
        },
        After: func(ctx context.Context, _ any, result any, actionErr error, _ *action.Meta) {
            // commit or release based on actionErr
        },
    }
}
```

---

## Checkpointing and resume

`Runner.Request.Store` is a `CheckpointStore`:

```go
type CheckpointStore interface {
    Save(ctx context.Context, cp Checkpoint) error
    Load(ctx context.Context, runID string) (Checkpoint, bool, error)
    Delete(ctx context.Context, runID string) error
}
```

Built-in implementations: `FileCheckpointStore` (atomic write, 0600),
`MemoryCheckpointStore`.

On failure the runner writes a checkpoint and prints a resume command:

```bash
nexssflow ./pipeline.flow --resume=run_1731000000
```

Resume replays the checkpoint's state and skips completed layers. A
`flow_hash` guard rejects a checkpoint if the flow has changed.

---

## Approval and HITL

```go
type ApprovalGate interface {
    Check(ctx context.Context, actionName, argsJSON, token string) error
}
```

`runner.TerminalApprovalGate` prompts on stdin. Modes:

- `danger` — prompts only for actions with `exec`, `write`, `delete`,
  `rm`, `drop`, `truncate`, `migration`, `patch`, or `deploy` in the name
- `all` — prompts for every action
- `none` — no prompts

Programmatic callers pass the token via `xctx.WithApprovalToken`.

A graph compiled with `Approval: true` nodes or
`Policy.ApprovalRequiredFor` will refuse to compile without a gate.

### Custom gates

Any type with a `Check` method satisfies `ApprovalGate`:

```go
type SlackApproval struct {
    channel string
}

func (s *SlackApproval) Check(ctx context.Context, actionName, args, token string) error {
    // send message to Slack, wait for reaction, return nil or error
}
```

---

## Journaling and replay

`journal.BranchJournal` records which edge was taken for every
conditional node. Replaying a run with the same `run_id` uses the
recorded decisions instead of re-evaluating conditions.

```go
type BranchJournal interface {
    Get(ctx context.Context, runID, sourceNode string) ([]BranchRecord, bool, error)
    Put(ctx context.Context, runID, sourceNode string, records []BranchRecord) error
}
```

Built-in implementations:

- `MemoryBranchJournal` — for tests
- `SQLBranchJournal` — SQLite-compatible schema (works with SQLite, Postgres, MySQL)

Enable by passing `flow.WithJournal(j)` to `NewCompiler` and setting
`xctx.WithExecutionID(ctx, runID)` on the run.

```go
db, _ := sql.Open("sqlite3", ":memory:")
j := journal.NewSQLBranchJournal(db)
_ = j.EnsureSchema(ctx)

compiler := flow.NewCompiler(reg, flow.WithJournal(j))
```

---

## Snapshots

`journal.SnapshotJournal` persists a run's state at every layer boundary
so a crashed run can be recovered from the last successful layer.

```go
type Snapshot struct {
    RunID       string
    StepIndex   int
    StateData   map[string]any
    SpentMicros int64
    Timestamp   time.Time
}
```

`FileSnapshotJournal` writes each step under
`<baseDir>/<runID>/step_NNNNN.json` and a `latest.json` pointer. Every
write is atomic and durable: temp file, fsync, rename, fsync directory.

```go
j, _ := journal.NewFileSnapshotJournal("./snapshots")
_ = j.Save(ctx, journal.Snapshot{
    RunID:     "run_42",
    StepIndex: 3,
    StateData: map[string]any{"step": 3},
})

snap, found, _ := j.Recover(ctx, "run_42")
```

---

## Observability

```go
sink := observe.NewPrometheusSink()

act := action.New("user.fetch", handler).
    AnyHook(observe.Hook(sink)).
    Build()
```

Built-in sinks:

| Sink | Purpose |
|---|---|
| `observe.NewSlogSink(logger)` | Structured `log/slog` output |
| `observe.NewMemorySink(cap)` | Thread-safe ring buffer of recent events |
| `observe.NewMetricsSink()` | Aggregate counters by action + kind |
| `observe.NewPrometheusSink()` | Prometheus exposition text |
| `observe.NewJSONLSink(w, maxBytes)` | Newline-delimited JSON |

Event kinds: `executed`, `error`, `retry`, `cache_hit`, `cache_miss`,
`canceled`, `panic`, `coalesced`, `deduplicated`.

Every event carries `ExecutionID`, `TraceID`, `SpanID`, `TenantID`,
`UserID`, `Duration`, and the full `Request`/`Response` payloads.

### Fan-out to multiple sinks

```go
type fanoutSink struct{ sinks []observe.Sink }

func (s *fanoutSink) Emit(ctx context.Context, e observe.Event) {
    for _, sink := range s.sinks {
        sink.Emit(ctx, e)
    }
}

sink := &fanoutSink{sinks: []observe.Sink{
    observe.NewSlogSink(slog.Default()),
    observe.NewMetricsSink(),
    observe.NewPrometheusSink(),
}}

act := action.New("user.fetch", handler).AnyHook(observe.Hook(sink)).Build()
```

---

## Telemetry hot path

`telemetry.HotPathExecutor` wraps a node and records one 64-byte ring
slot per invocation. The ring buffer is lock-free SPSC, cache-line
padded, and allocates nothing per push.

```go
ring := ringbuf.New(8192)
exec := telemetry.NewHotPathExecutor(ring)

out, err := exec.ExecuteNode(ctx, nodeID, invoker, payloadBytes)

// batch drain into a caller-owned slice
var batch [64]ringbuf.Slot
n := ring.BatchDrain(batch[:])
```

---

## State and conditions

`flow.NewState(map[string]any)` wraps a run's mutable state. `State.Get`
walks nested paths and struct fields, supporting `snake_case` JSON tags
as well as Go field names.

`flow.EvaluateCondition(expr, state)` evaluates a graph edge condition:

```
state.tests_passed == true
state.score >= 0.85
state.error exists
state.status != "pending"
```

Supported operators: `==`, `!=`, `>`, `>=`, `<`, `<=`, `exists`.
Literals: `"string"`, `'string'`, `true`, `false`, numbers.

---

## YAML graph definition

An alternative to the Arrow DSL. Same compiler, same runtime.

```yaml
apiVersion: nexss.ai/v1
kind: Graph
metadata:
  name: review_pipeline
  version: "1.0.0"

policy:
  max_parallel_nodes: 8
  max_context_bytes: 1048576
  budget_micros: 5000000
  approval_required_for: ["high_risk"]
  fan_in_recovery:
    strategy: retry_failed
    max_attempts: 3
    backoff_ms: 200
    max_backoff_ms: 5000
    retry_transient_only: true

nodes:
  - id: fetch
    capability: user.fetch
    kind: tool
    timeout_ms: 5000
    retry: { max_attempts: 2, backoff: exponential }
  - id: review
    capability: user.review
    kind: llm
    estimate_micros: 50000
    effect: read_only

edges:
  - from: fetch
    to: review
    when: 'state.tier == "pro"'
    priority: 1
  - from: fetch
    to: notify
    otherwise: true
    priority: 99
```

Load with `flow.LoadYAML(data)` or `flow.LoadYAMLFile(path)`.

- Node kinds: `deterministic`, `llm`, `tool`, `subgraph`, `human`, `approval`
- Branch modes: `first_match` (default), `all_matches`
- Fan-in recovery: `fail_fast`, `retry_failed`, `continue_partial`

---

## Learning router

A multi-armed bandit that routes each call to one of N candidate
actions, learning from reward signals.

```go
router := learn.NewRouter(learn.RouterConfig{
    Name:         "gateway.router",
    Temperature:  1.8,
    LearningRate: 0.15,
    RewardFn:     myReward,
}, providerFast, providerCheap, providerReliable)

out, err := router.DoAny(ctx, payload)
```

`RewardFn` receives the result, error, and duration; returns a float64
reward.

```go
rewardFn := func(res any, err error, d time.Duration) float64 {
    if err != nil {
        return -50.0
    }
    return 20.0 - float64(d.Milliseconds())/10.0
}
```

The router uses online softmax Q-learning and warms up by visiting each
candidate once.

---

## Evolutionary optimizer

Searches for the optimal pipeline topology by mutating a baseline DSL
across generations.

```go
best, err := optimizer.Evolve(ctx, baselineDSL, reg, evaluator,
    optimizer.Options{Generations: 4, Population: 6})
```

Mutations: add `:retry=N`, wrap two adjacent nodes in `( A & B )`, add
a fallback with `||`.

```go
evaluator := func(ctx context.Context, candidate action.AnyAction) (float64, error) {
    start := time.Now()
    _, err := candidate.DoAny(ctx, myPayload)
    if err != nil {
        return -500.0, nil
    }
    return 100.0 - float64(time.Since(start).Milliseconds()), nil
}
```

The result is a candidate DSL string and its fitness score.

---

## Extending Flow

### Adding a custom node

Any `action.AnyAction` from `nexssp/kernel` can appear in a flow. Build
one with `action.New` and register it:

```go
myNode := action.New("my.custom_node", func(ctx context.Context, req MyReq) (MyRes, error) {
	return MyRes{}, nil
}).
	Description("Does something custom").
	Tag("custom").
	Build()

reg := action.MustNewRegistry(action.Of(myNode))

```

### Adding a custom transport

Implement `transport.Transport`:

```go
type Transport interface {
    fmt.Stringer
    CanHandle(b action.Binding) bool
    Mount(actions []action.AnyAction)
    Do(ctx context.Context, v any) (any, error)
}
```

Register it with the app builder via `WithLoader`:

```go
app.WithLoader(func(asm *bootstrap.Assembly) error {
    myTransport := myt.New()
    myTransport.Mount(asm.Actions)
    return nil
})
```

### Adding a hook

Hooks run before and after every node. Use them for logging, auditing,
cost tracking, rate limiting, or anything that wraps the call:

```go
auditHook := action.AnyHook{
    Before: func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
        log.Printf("calling %s", meta.Name)
        return ctx, nil
    },
    After: func(ctx context.Context, req, res any, err error, meta *action.Meta) {
        if err != nil {
            log.Printf("%s failed: %v", meta.Name, err)
        }
    },
}

compiler := flow.NewCompiler(reg, flow.WithHooks(auditHook))
```

### Adding a middleware

Kernel middlewares wrap a single action's handler:

```go
wrapped := action.New("my.op", handler).
    Timeout(5 * time.Second).
    Retry(3, action.ExponentialJitter(100*time.Millisecond, 2*time.Second)).
    Cache(1*time.Minute, func(r MyReq) string { return r.Key }).
    Dedup(func(r MyReq) string { return r.Key }).
    RateLimit(100, 200).
    ConcurrencyLimit(16).
    Build()
```

Every middleware that appears in the DSL is a kernel middleware. To add
a new one, add the modifier parser branch in `dslparse/` and the
corresponding `Builder` method in `kernel/action`.

### Adding a graph node kind

Node kinds are strings validated by `definition.go`. To add a new kind
(e.g. `node_webhook`), extend `NodeKind` and add a case in the compiler's
`resolveCapability` switch.

### Adding a sink

```go
type Sink interface {
    Emit(context.Context, Event)
}
```

Register with the compiler via `flow.WithHooks(observe.Hook(mySink))`,
or attach to individual actions with `.AnyHook(observe.Hook(mySink))`.

### Adding a checkpoint store

Implement `CheckpointStore`. Use any storage backend: S3, Redis,
Postgres, memory.

### Adding an approval gate

Implement `ApprovalGate`. Wire it with `flow.WithApprovalGate(myGate)`.

### Adding a journal backend

Implement `journal.BranchJournal` or `journal.SnapshotJournal`. SQL,
file, memory, or anything else.

### Adding a config knob

Extend `Config` in `config.go`, add the parser branch in each of
`config_cli.go`, `config_env.go`, `config_dsl.go`, and add the key to
`knobKeys`.

---

## See also

- `FLOW.en.md` — design rationale and manifesto
- `examples/` — runnable `.flow` files and Go examples
- `showcase/` — adaptive router, hot swap, evolutionary optimizer,
  multi-tenant governance
- `runner/` — the runner package in isolation

*Apache License 2.0. Copyright © 2018–2026 Marcin Polak and Contributors.*
