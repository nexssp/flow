# Nexss Flow cookbook

This cookbook is based on the current Flow compiler, native bundle registration, and executable `.nflow` fixtures in this repository. It shows practical patterns, but each example still depends on the capabilities mounted by the host. Run `nflow catalog --flow FILE` to inspect the active registry.

The standard CLI bundle set is assembled in [`native/native.go`](../native/native.go). Optional examples are marked explicitly.

## 1. Run, inspect, format, and lint a flow

```sh
nflow run flow.nflow
nflow run flow.nflow '{"user_id":42}'
nflow run -vv flow.nflow
nflow show flow.nflow
nflow catalog --flow flow.nflow
nflow lint flow.nflow
nflow fmt --write flow.nflow
```

From a source checkout, use `go run ./cmd/nflow ...` instead of `nflow ...`. `lint` validates the preprocessed source against the active registries but does not execute actions.

For post-run checks:

```sh
nflow run flow.nflow --assert='result.ok == true'
```

## 2. Produce, pass, and shape data

### Produce a scalar or object

```nflow
const :timeout=3s :retry=2 @{ value: "Julia" }
```

```nflow
const @{ user_id: 42, active: true }
```

Use `value` for a scalar, array, or an object that must remain under one field. Direct object mode is convenient for object results.

### Pipe data into actions

```nflow
const @{ value: "Julia" } -> wrap @{ key: "greeting" }
```

`|` is an alias for `->`:

```nflow
const @{ value: "Julia" } | wrap @{ key: "greeting" }
```

### Project fields

```nflow
{ id: .user.id, name: .user.name, active: .user.active }
```

Projection shorthand copies a field with the same name:

```nflow
{ id, name, email }
```

Spread the current object before overriding fields:

```nflow
{ ..., status: "normalized" }
```

Local bindings are supported in projection bodies:

```nflow
{
  let total = .price * .quantity;
  ..., total: total, currency: "EUR"
}
```

### Select and wrap

```nflow
{ user: { id: 42, name: "Julia" } }
-> pick @{ field: "user.id" }
-> wrap @{ key: "data" }
```

For a dynamic field path, keep the path in the action request. `pick` accepts a field/path string; the exact behavior is implemented by `extensions/runtime/pick.go`.

## 3. Branching and concurrency

### Conditional expression

```nflow
.result == "ok" ? const @{ value: "accepted" } : const @{ value: "rejected" }
```

The else arm is optional:

```nflow
.result.ready ? debug
```

### Concurrent branches with `&`

```nflow
(
  const @{ value: "profile" }
  & const @{ value: "metrics" }
)
```

The result is keyed by canonical action name. Repeated names receive numbered keys such as `runtime.const#1` and `runtime.const#2`.

### Stable labels with `parallel`

```nflow
parallel {
  profile: const @{ value: "profile-ready" },
  metrics: const @{ value: "metrics-ready" },
}
```

Labels must be unique and non-empty. A failed branch fails the parallel action; partial results are not passed to the next pipeline step.

Direct literal branches are shorter. Use `const` when the branch needs action
arguments or modifiers:

```nflow
parallel {
  profile: "profile-ready",
  metrics: const :timeout=2s @{ value: "metrics-ready" },
}
```

### First successful result with `race`

```nflow
race {
  service.primary @{ region: "east" },
  service.replica @{ region: "west" },
}
```

All candidates start concurrently. The first successful result wins and cancellation is requested for the remaining candidates. A candidate that ignores cancellation can delay completion.

## 4. Errors and recovery

### Fallback with `||`

```nflow
fail @{ kind: "Unavailable", message: "primary failed" }
|| const @{ value: "fallback" }
```

`||` catches an error from its left branch generally. It is not a typed error matcher.

### Short error handler

```nflow
request.primary
? on_error request.backup
```

### Error routing block

```nflow
request.primary
? on_error {
  when .kind == "Unavailable" -> request.backup,
  else -> error.reraise,
}
```

The exact handler target must be available in the active registry. Standard caller cancellation is not recovered by ordinary error handlers.

### Inspect or rethrow the current error

```nflow
request.primary
? on_error {
  when .kind == "Unavailable" -> error.info,
  else -> error.reraise,
}
```

Fixtures: [`extensions/on_error/nflows/`](../extensions/on_error/nflows/).

## 5. Match and bounded loops

### Match predicates

```nflow
match {
  .danger && .level > 3 -> const @{ value: "critical" },
  .danger -> const @{ value: "warning" },
  _ -> const @{ value: "nominal" },
}
```

### Match a subject

```nflow
match(.score) {
  > 90 -> const @{ value: "high" },
  >= 50 -> const @{ value: "medium" },
  _ -> const @{ value: "low" },
}
```

The first matching arm wins. `_` is the default arm. Predicate errors are handled according to the match extension’s condition evaluation rules; see [`extensions/match/conditions.go`](../extensions/match/conditions.go).

### Loop until a condition

```nflow
{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )
```

The compiled loop is bounded to 15 iterations. It is not an unbounded `while` loop. Use an accumulator projection when the loop body must preserve state.

## 6. Reusable pipelines and modules

### Declare and call a pipeline

```nflow
@pipeline greet
  :tag="example"
  const @{ value: "hello" }
@end

greet
```

A pipeline can receive a complete named request:

```nflow
@pipeline greet
  :request-schema=Greeting
  { message: .name }
@end

greet @{ name: "Julia" }
```

### Include a local module

`main.nflow`:

```nflow
@include "billing.nflow" AS billing

billing.pipeline.lookup @{ account_id: "A-42" }
```

`billing.nflow`:

```nflow
@pipeline lookup
  debug
@end
```

`@include` is a source module import, not textual expansion. Imported pipelines are qualified under the include namespace.

### Define a macro

```nflow
@macro label(value) {
  wrap @{ key: "label", value: value }
}

const @{ value: "ready" } -> @label("status")
```

Macros are compile-time source substitutions. Recursive definitions and excessive expansion are rejected.

## 7. Compile-time directives

### Constants

```nflow
@const API_NAME = "orders"
@const RETRIES = 3
```

Use compile-time constants in supported directive and source positions. For structured JSON constants:

```nflow
@const.load "settings.json" as settings
```

### Configuration

```nflow
@config {
  strict: true
  service_url: "https://api.example.test"
}
```

Load a JSON configuration file:

```nflow
@config.load:path="settings.json" :prefix="app" :required=true
```

YAML loading requires the optional `config_yaml` bundle.

### File-local symbols

```nflow
@symbols status {
  pending
  complete
}

const :tag="status" @{ value: status.complete }
```

Symbols also accept comma-separated names on one line, whitespace-separated
bare names, explicit mappings, and an explicit empty value after a colon. When
mixing a bare name with a `name:` entry, use a comma:

```nflow
@symbols labels { pending done }
@symbols status { pending, done: }
@symbols roles { reviewer: "reviewer", operator: "operator" }
```

`status.pending` resolves to the string `"pending"`; `status.done` resolves to
the empty string because `done:` is an explicit empty mapping. The mixed form
`@symbols status { pending done: }` is invalid because `pending done` is read as
one symbol name.

Typed symbol values are supported:

```nflow
@symbols limits {
  timeout: duration = 5s
  attempts: int = 3
  enabled: bool = true
}
```

Symbols are file-local and can be shadowed by module scope rules. See [`symbols.md`](symbols.md).

### Version constraint

```nflow
@nflow_version ">=0.37.0 <0.40.0"
```

Only the registered `@nflow_version` spelling is valid in the current source.

## 8. Schemas and assertions

### Declare and attach a request schema

```nflow
@schema Request {
  Name string `json:"name" validate:"required"`
  Depth int `json:"depth" default:"1" validate:"gte=1"`
}

@pipeline handle
  :request-schema=Request
  debug
@end
```

The shorter boundary aliases are also supported:

```nflow
@pipeline handle
  :in-schema=Request
  :out-schema=Response
  debug
@end
```

Pipeline-level `:schema=...` is not a current alias. At the atom level it is
metadata; use `NAME.validate` for an explicit mid-pipeline validation step.

Schemas validate supported scalar, slice, map, pointer, and nested struct-shaped values at the relevant pipeline/action boundary. Schema tags are parsed by the Flow schema extension.

### Runtime assertion

```nflow
const @{ value: 3 }
-> assert(. >= 2, "value must be at least two")
```

### Post-run assertion

```nflow
const @{ ok: true }
@assert: result.ok == true
```

Multiline form:

```nflow
const @{ items: ["a", "b"] }
@assert {
  len(result.items) == 2
  all(result.items, # != "")
}
```

Use `@assert` for a completed-run contract. Use `assert(...)` for an in-pipeline check.

## 9. Policies and modifiers

Modifiers are attached with `:name` or `:name=value`:

```nflow
http.request :timeout=3s :retry=2 :cache=1m
```

Common standard modifiers include:

```nflow
worker :timeout=5s
worker :retry=3
worker :concurrency=8
worker :cache=30s
worker :coalesce
worker :dedup
worker :idempotent
worker :rate_limit=100
worker :auth
worker :role="reviewer"
worker :permission="orders.read"
worker :feature="new-checkout"
worker :strict
worker :lenient
worker :tag="billing"
worker :status=201
```

The exact action compatibility and value validation come from the active modifier registry. Flow maps these policies to Kernel action builders; it does not implement a separate retry or circuit-breaker engine.

### Reuse a policy profile

```nflow
@profile reliable
  :timeout=2s
  :retry=4

@pipeline fetch
  :profile=reliable
  http.request
@end
```

Nested scopes apply inheritable policies:

```nflow
@scope :timeout=10s {
  first.action
  @scope :timeout=2s {
    second.action
  }
}
```

An atom’s own modifier wins; nested scopes override by modifier name.

## 10. Streams and files

Generic stream operators are expression-aware adapters over Kernel stream mechanics:

```nflow
cov.items -> stream.map @{ expression: "item + item" } -> collect
```

`cov.items` is a self-test-only source. Replace it with a mounted source such as
`io.stdin` in a host that provides that source. The generic operators are
expression-aware; their exact request fields are visible in the live catalog.

Use the source/operator schemas shown by `nflow catalog --flow FILE`; stream argument shapes are action-specific.

Filesystem examples:

```nflow
fs.walk @{ root: "./data" }
-> fs.read
-> fs.filter @{ expression: ".size > 0" }
-> fs.sort @{ by: "name" }
-> out.file @{ path: "./out/results.jsonl" }
```

Filesystem, stdout, clipboard, and rendering actions remain domain capabilities. They use the host’s configured resolver and filesystem policy.

## 11. Fan-out, dispatch, and pools

### Dispatch through named members

```nflow
@pool coverage [noop]
dispatch.run @{ members: [pool.coverage], payload: { value: "ready" } }
```

The members must resolve as action capabilities in the active registry.

### Declare a pool

```nflow
@pool workers [service.a, service.b] { strategy: "round_robin" }

pool.workers
```

Other strategies are `failover` and `hash`; hash routing requires a key path. Pool execution is backed by Kernel routers.

### Map one action over items

```nflow
distribute.map @{ action: noop, items: [1, 2, 3] }
```

The action field is a capability reference, not a string. Additional fields
such as concurrency and failure policy are host/action-schema dependent; use
`nflow catalog --flow FILE` for the mounted schema.

`distribute.map` preserves input order and returns Flow’s per-item result envelope. Kernel owns the bounded fan-out mechanics; Flow owns request decoding and result shaping.

## 12. External and host-dependent actions

The standard bundle includes external action adapters, but the host still controls availability and permissions:

```nflow
external.exec @{ command: "printf ready" }
```

HTTP, WASM, language runners, NATS, configuration backends, and other integrations may require additional registration or `@require`. Inspect the live catalog rather than assuming a repository directory is mounted.

Optional embedded files require the `embed` bundle and an explicit host filesystem root:

```nflow
@embed prompt "prompts/system.txt"
const @{ value: embed.prompt }
```

## 13. Approval, hooks, and event metadata

### Approval gate

```nflow
@approval review {
  role: reviewer
  reason: "release to production"
  expires: 1h
}

approval.review
```

Run with a matching token:

```sh
nflow run release.nflow --approval review --journal-dir .flow-state
```

File-backed resume is local to the journal process and is not a distributed lock service.

### Attach a registered hook

```nflow
@hook:hook.verify,observe.metrics
```

Every named hook must exist in the active Kernel registry.

### Event declaration

```nflow
@on event "nats:orders.incoming"
```

This records event metadata. The stock CLI does not automatically start a listener for every event declaration.

## 14. Practical debugging checklist

1. Run `nflow catalog --flow FILE` and confirm every action, source, operator, directive, and modifier is mounted.
2. Run `nflow lint FILE` before debugging runtime behavior.
3. Use `nflow run -vv FILE` for action names and `-vvv` for payload-level tracing.
4. Use `nflow show FILE` or `nflow explain` to inspect compiled structure where supported by the CLI version.
5. If a flow uses `@require`, verify the module/version and harness build output.
6. If an approval or idempotent flow behaves unexpectedly, use a fresh execution ID when the source or callsite layout changed.
7. Keep tests deterministic: use `@assert` for run contracts and `nflow test` with `@test` for suite-only checks.

## Source fixtures

The most useful executable examples are under:

- [`extensions/syntax/nflows/`](../extensions/syntax/nflows/)
- [`extensions/projection/nflows/`](../extensions/projection/nflows/)
- [`extensions/pipeline/nflows/`](../extensions/pipeline/nflows/)
- [`extensions/schema/nflows/`](../extensions/schema/nflows/)
- [`extensions/on_error/nflows/`](../extensions/on_error/nflows/)
- [`extensions/stream_ops/nflows/`](../extensions/stream_ops/nflows/)
- [`extensions/nodes_distribute/nflows/`](../extensions/nodes_distribute/nflows/)
- [`extensions/modifiers_core/nflows/`](../extensions/modifiers_core/nflows/)

For the complete implementation-backed syntax, continue to [`language-reference.md`](language-reference.md).
