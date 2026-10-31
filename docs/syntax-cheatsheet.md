# Nexss Flow syntax cheatsheet

Source-verified against the current Flow checkout. A construct is available only when its bundle is mounted; the stock CLI bundle list is defined in [`native/native.go`](../native/native.go).

## Run and inspect

```sh
nflow run flow.nflow
nflow run flow.nflow '{"id":42}'
nflow run -vv flow.nflow
nflow lint flow.nflow
nflow fmt --write flow.nflow
nflow catalog --flow flow.nflow
nflow show flow.nflow
nflow run flow.nflow --assert='result.ok == true'
```

## Core atoms and values

```nflow
noop
const @{ value: "hello" }
const @{ count: 3, enabled: true, tags: ["a", "b"] }
request.action :timeout=3s :retry=2 @{ id: .id, payload: . }
```

`@{ ... }` is a structured data block. Use explicit `key: value` pairs. Values can be literals, arrays, nested objects, `.`, and input paths such as `.user.id`.

Modifiers and `@{ ... }` may be combined on the same action. Modifiers come
before the argument block; both compact and spaced colon forms are accepted:

```nflow
request.action:timeout=3s:retry=2 @{ id: .id }
request.action :timeout=3s :retry=2 @{ id: .id }
```

## Built-in keyword actions

| Keyword | Canonical action |
|---|---|
| `const` | `runtime.const` |
| `with` | `runtime.with` |
| `pick` | `runtime.pick` |
| `wrap` | `runtime.wrap` |
| `fail` | `fail` |
| `noop` | `noop` |
| `sleep` | `runtime.sleep` |
| `env` | `runtime.env` |
| `uuid` | `runtime.uuid` |
| `json` | `json.clean` |

## Pipelines and control flow

```nflow
left -> right
left | right
left || fallback
(left & right)
parallel { one: left, two: right }
race { primary, backup }
condition ? yes : no
```

- `->` and `|` pass the left result to the right action.
- `||` runs the right side after a left-side error.
- `&` gathers concurrent branches under canonical action names.
- `parallel` gathers concurrent branches under explicit labels.
- `race` returns the first successful candidate.
- The ternary else arm is optional.

Named parallel branches may use direct literal values. Use `const` when a
branch needs action arguments or modifiers:

```nflow
parallel {
  profile: "profile-ready",
  metrics: const :tag="metrics" @{ value: "metrics-ready" },
}
```

## Data shaping

```nflow
{ id: .user.id, name: .user.name }
{ id, name, email }
{ ..., status: "ready" }
{ let total = .price * .quantity; ..., total: total }
pick @{ field: "user.id" }
wrap @{ key: "data" }
```

## Match and loops

```nflow
match {
  .ok && .score > 90 -> const @{ value: "high" },
  .ok -> const @{ value: "normal" },
  _ -> const @{ value: "reject" },
}

match(.score) {
  > 90 -> const @{ value: "high" },
  _ -> const @{ value: "normal" },
}

{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )
```

`match` chooses the first matching arm. `_` is the default. Flow bounds the compiled loop to 15 iterations.

## Errors and assertions

```nflow
request.primary || request.backup
request.primary ? on_error request.backup
request.primary ? on_error {
  when .kind == "Unavailable" -> request.backup,
  else -> error.reraise,
}

value -> assert(. >= 0, "must be non-negative")
```

Post-run assertions:

```nflow
const @{ ok: true }
@assert: result.ok == true
@assert {
  result.ok == true
  duration_ms < 1000
}
```

## Reusable declarations

```nflow
@pipeline greet
  const @{ value: "hello" }
@end

greet

@include "billing.nflow" AS billing
billing.pipeline.lookup @{ account_id: "A-42" }

@macro label(value) {
  wrap @{ key: "label", value: value }
}
```

## Compile-time declarations

```nflow
@const API = "orders"
@const.load "settings.json" as settings
@config { strict: true }
@config.load:path="settings.json" :prefix="app"
@nflow_version ">=0.37.0 <0.40.0"

@symbols status {
  pending
  done
}

@schema Request {
  Name string `json:"name" validate:"required"`
  Depth int `json:"depth" default:"1" validate:"gte=1"`
}
```

Bare names may be comma-separated or whitespace-separated on one line. When a
bare name is mixed with an explicit `name:` entry, use a comma. An explicit
`name:` entry with no value means an empty string:

```nflow
@symbols labels { pending done }
@symbols status { pending, done: }
@symbols roles { reviewer: "reviewer", operator: "operator" }
```

Therefore `status.pending` is `"pending"`, while `status.done` is `""`.
`@symbols status { pending done: }` is invalid because `pending done` is read
as one symbol name.

## Policy modifiers

```nflow
worker :timeout=5s
worker :retry=3
worker :cache=1m
worker :concurrency=8
worker :coalesce
worker :dedup
worker :idempotent
worker :rate_limit=100
worker :auth :role="reviewer" :permission="orders.read"
worker :feature="new-checkout"
worker :strict
worker :lenient
worker :tag="billing" :status=201
```

Profiles and scopes:

```nflow
@profile reliable
  :timeout=2s
  :retry=4

@scope :timeout=10s {
  request.action
}
```

## Pools, dispatch, and distribution

```nflow
@pool workers [service.a, service.b] { strategy: "round_robin" }
pool.workers

dispatch.run @{ members: [pool.workers], payload: . }

distribute.map @{ action: noop, items: [1, 2, 3] }
```

Pool strategies are `round_robin`, `failover`, and `hash`; hash routing requires a key path. `distribute.map` returns a Flow result envelope and uses Kernel bounded fan-out internally.

## Streams and I/O

```nflow
cov.items -> stream.map @{ expression: "item + item" } -> collect
fs.walk @{ root: "./data" } -> fs.read
io.stdin -> stream.take @{ count: 20 } -> collect
```

`cov.items` is self-test-only. Use `nflow catalog --flow FILE` to confirm available stream sources and operator schemas.

## Approval, hooks, and events

```nflow
@approval review {
  role: reviewer
  reason: "release to production"
  expires: 1h
}
approval.review

@hook:hook.verify,observe.metrics
@on event "nats:orders.incoming"
```

`@on` records event metadata; the stock CLI does not automatically start a listener for every declaration. `@approval` durable resume requires a suspension-capable journal.

## Optional bundles

```nflow
@require github.com/nexssp/lang/nexssflow vX.Y.Z
@require github.com/nexssp/config_yaml vX.Y.Z
@embed prompt "prompts/system.txt"
```

`@embed` requires the optional embed bundle and an explicit host filesystem root. External language execution is not sandboxed; only run trusted source.

## Syntax boundaries to remember

- `@{ ... }` is a data-value block, not an arbitrary expression block.
- `@const` is compile-time; `const` is a runtime keyword action.
- `@config` is a directive; an atom-level `@config` marker is a separate configuration-injection feature.
- `@include` imports source modules; it is not textual expansion.
- `@assert` runs after execution; `assert(...)` runs inside the pipeline.
- `@test` is mounted by `nflow test`, not ordinary `nflow run`.
- Capabilities in examples such as `http.request`, `service.a`, and `cov.items` require the corresponding host or test bundle.

For full details and source links, see [`language-reference.md`](language-reference.md) and [`cookbook.md`](cookbook.md).
