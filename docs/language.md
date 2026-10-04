# The `.nflow` language

A `.nflow` file is a graph of actions with typed policies and reusable
patterns. This document is the language reference; for the CLI that runs
these files, see [cli.md](cli.md).

Every snippet on this page is a real, working flow. Copy any of them
into a file and run it:

```sh
nflow run my-flow.nflow
```

---

## 1. Pipeline syntax

The pipeline is the flow. There are five operators and one literal:

| Form | Meaning |
|---|---|
| `a -> b` | Pass the output of `a` into `b` |
| `a \| b` | Sugar for `a -> b` |
| `a \|\| b` | Run `b` if `a` fails |
| `(a & b)` | Run `a` and `b` concurrently, gather both |
| `{ key: expr, ... }` | Project a new object from the current input |
| `a ? b : c` | Conditional: `b` when `a` is truthy, else `c` |

The smallest pipeline:

```nflow
noop
```

A pipeline that produces a literal:

```nflow
const @{ value: "hello" }
```

A pipeline that shapes the input:

```nflow
{ id: .user.id, name: .user.name }
```

A pipeline that fans out and gathers:

```nflow
(
  const @{ value: "left" }
  &
  const @{ value: "right" }
)
```

Result: `{ "runtime.const#1": "left", "runtime.const#2": "right" }`.
Parallel branches key their output by the branch action's canonical name;
repeats get `#1`, `#2`, and so on in source order.

Fallback:

```nflow
fail @{ kind: "Unavailable", message: "boom" } || const @{ value: "recovered" }
```

`fail` raises; `||` catches and runs the right arm.

---

## 2. Native keywords

Ten keyword names are part of the language itself. They compile to
canonical actions at parse time and cannot be overridden by any bundle.

| Keyword | Compiles to | Meaning |
|---|---|---|
| `const` | `runtime.const` | Produce a literal |
| `with` | `runtime.with` | Merge fields into the input |
| `pick` | `runtime.pick` | Select fields from the input |
| `wrap` | `runtime.wrap` | Wrap the input under a key |
| `fail` | `runtime.fail` | Raise an error |
| `noop` | `runtime.noop` | Pass through unchanged |
| `sleep` | `runtime.sleep` | Delay by `duration_ms` |
| `env` | `runtime.env` | Read an environment variable |
| `uuid` | `runtime.uuid` | Generate a UUID v4 |
| `json` | `json.clean` | Strip markdown fences and prose from a JSON body |

Both forms are valid; prefer the keyword.

```nflow
{ user_id: 42 } -> pick @{ field: "user_id" } -> wrap @{ key: "data" }
```

**Keywords are grammar, not aliases.** The canonical form
(`runtime.const`) remains valid and is what you'll see in
`nflow explain` output, but a bundle cannot register an action named
`const` — the mount rejects it.

---

## 3. Arguments: `@{ ... }`

`@{ }` binds named values to an action. The values are parsed as data,
not as a mini-language:

```nflow
{ user: { id: 42, name: "Ada" } }
-> wrap @{ key: "payload", note: "wrapped", count: 3, active: true }
```

Inside `@{ }`:

| Form | Meaning |
|---|---|
| `key: "text"` | String literal |
| `key: 42` | Number |
| `key: true` | Boolean |
| `key: [1, 2, 3]` | Array |
| `key: { a: 1 }` | Nested object |
| `key: .path.to.field` | Reference to the current input |
| `key: .` | The whole input |
| `key: noop` | A capability reference (see §4) |

References read from the current pipeline state — the value flowing
into this atom.

---

## 4. Capability references

Some arguments name another capability. Write the name as a bare
identifier; the compiler resolves it:

```nflow
distribute.map @{ action: noop, items: [1, 2, 3] }
```

`noop` here is a **capability reference**, not a string. It is
translated to `runtime.noop`, verified against the registry, and passed
to the handler as the canonical name. Quoting it — `action: "noop"` —
is a compile error:

```
distribute.map @{ action: "noop" }
  error: use a bare capability reference, not a string literal ("noop")
```

Bare identifiers in **undeclared** argument fields stay as plain
strings. `const @{ value: noop }` produces the string `"noop"`, not a
capability. The compiler only treats an argument as a capability
reference when the owning bundle declares it as one.

Canonical names (`runtime.noop`) are also accepted in capability
positions.

---

## 5. Projections: `{ ... }`

A `{ ... }` at the top of a pipeline reshapes the current value:

```nflow
{ id: 1, name: "Ada", extra: "discard" }
-> { name: .name, greeting: "Hello, " + .name }
```

Result: `{ "name": "Ada", "greeting": "Hello, Ada" }`.

Projections can spread the whole input with `...`:

```nflow
{ id: 42, status: "pending" }
-> { ..., status: "active" }
```

Result: `{ "id": 42, "status": "active" }`.

Projections run through `expr-lang`, so array functions like `filter`,
`sortBy`, `map`, and `take` are available:

```nflow
{ items: [{ line: 30, severity: "high" }, { line: 10, severity: "low" }] }
-> { top: items | filter(#.severity == "high") | sortBy(#.line, "desc") | take(1) }
```

`{ ... }` is data. `noop` inside a projection is a variable lookup, not
a capability. Use `@{ }` for capability references.

---

## 6. Directives

Directives start with `@` at the beginning of a line. They run before
the pipeline is compiled.

| Directive | Purpose |
|---|---|
| `@description "..."` | Human-readable summary; shown by `nflow info`, used as the fixture name |
| `@assert: expression` | Assertion evaluated after the pipeline runs |
| `@pipeline NAME` ... `@end` | Declare a named, reusable sub-pipeline |
| `@scope :mods { ... }` | Inline policy for the enclosed atoms |
| `@profile NAME :mods` | Named, reusable policy |
| `@macro NAME(params) { body }` | Compile-time pattern expansion |
| `@require PATH [version] [as alias] [{ options }]` | Load an external bundle |
| `@config { ... }` / `@config:key=value` | Compile-time key/value pairs |
| `@config.load:path="..."` | Load config from a JSON/TOML/YAML file |
| `@include "path.nflow"` | Inline another file |
| `@on_error { when ... -> target }` | Route a runtime error to a recovery target |
| `@hook:name` | Attach a Kernel hook to the compiled program |
| `@schema NAME { Field Type `tags` }` | Runtime payload validation for strict mode |

---

## 7. `@pipeline` — reusable fragments

```nflow
@pipeline fetch_user
  http.request @{ url: "https://api.example/users/1" }
  -> { id: .body.id, name: .body.name }
@end

{} -> pipeline.fetch_user
```

Every `@pipeline NAME` mounts as `pipeline.NAME`. Use that name to call
it. Modifiers on the pipeline header land on the wrapper:

```nflow
@pipeline fetch_user :tag="api":status=201
  ...
@end
```

Only policy modifiers (`:timeout`, `:retry`, ...) also propagate to the
atoms inside the body. Metadata modifiers (`:tag`, `:status`, `:route`)
stay on the wrapper — see §10 for the full rule.

---

## 8. `@scope` — inline policy

`@scope :mods { ... }` applies a policy to every atom inside the block.

```nflow
@scope :timeout=5s :retry=3 {
  http.request @{ url: "https://api.example/one" }
  http.request @{ url: "https://api.example/two" }
}
```

Both `http.request` atoms get `:timeout=5s :retry=3`.

Nested scopes override on a per-name basis:

```nflow
@scope :timeout=2s :retry=4 {
  http.request @{ url: "https://fast.example" }

  @scope :timeout=30s {
    http.request @{ url: "https://slow.example" }
  }
}
```

The first call gets `timeout=2s retry=4`; the second gets
`timeout=30s retry=4` (timeout overridden, retry inherited).

An atom-local modifier always wins:

```nflow
@scope :timeout=5s {
  http.request:timeout=1s @{ url: "..." }
}
```

That call gets `timeout=1s`.

---

## 9. `@profile` — named policy

A profile is a named, reusable `@scope`. Declare once, use anywhere.

```nflow
@profile reliable :timeout=10s :retry=4
@profile fast     :timeout=500ms :retry=0

@pipeline call_api :profile=reliable
  http.request @{ url: "https://api.example" }
@end

@pipeline probe :profile=fast
  http.request @{ url: "https://health.example" }
@end
```

A profile can inherit from one parent:

```nflow
@profile base     :timeout=10s :retry=4
@profile careful  :parent=base :timeout=30s
```

`careful` gets `timeout=30s retry=4`. Cycles and unknown parents are
compile errors:

```
@profile a :parent=b
@profile b :parent=a
  error: @profile a: profile cycle: a → b → a
```

---

## 10. Policy precedence

From highest priority to lowest:

```
action-local modifier       atom:timeout=1s
  > innermost @scope        @scope :timeout=2s { ... }
  > outer @scope
  > @pipeline local mods    @pipeline p :timeout=5s
  > @pipeline :profile
  > file-level @profile     @profile p :timeout=10s
```

**More local wins.** Later-declared (inner) scope spans override earlier
(outer) spans on the same modifier name.

Not every modifier propagates. Only **inheritable** policy modifiers
do:

```
:timeout  :retry  :cache  :dedup  :coalesce  :rate_limit  :concurrency  :idempotent
```

Metadata modifiers stay on the atom or wrapper where they're declared:

```
:tag  :status  :route  :name  :desc  :scope  :read_only  :audit  :debug  :deprecated
```

`:retry=0` is an explicit disable; it overrides any inherited
`:retry=N`. Absent means inherit.

---

## 11. `@macro` — compile-time patterns

A macro is a named expansion. The compiler substitutes parameters and
re-parses the body as if it were written at the call site.

```nflow
@macro user_card(name) {
  const @{ value: { name: $name } } -> wrap @{ key: "card" }
}

@user_card("Ada")
```

Expands to:

```nflow
const @{ value: { name: "Ada" } } -> wrap @{ key: "card" }
```

### Patterns

**Parameterless:**

```nflow
@macro hello() {
  const @{ value: "hello" }
}
@hello()
```

**Parameter substitution into a shape:**

```nflow
@macro labeled(name, amount) {
  const @{ value: { label: $name, amount: $amount } }
}
@labeled("total", 42)
```

**Expanding to multiple atoms:**

```nflow
@macro fetch_and_extract(url) {
  http.request @{ url: $url } -> { id: .body.id, name: .body.name }
}
@fetch_and_extract("https://api.example/users/1")
```

**Reusable guards:**

```nflow
@macro require_id() {
  assert(.id != nil, "id is required")
}
{ id: "usr_1" } -> @require_id() -> { ok: true }
```

**Inside a `@pipeline`:**

```nflow
@macro constant_answer() { const @{ value: 42 } }
@pipeline compute
  @constant_answer()
@end
{} -> pipeline.compute
```

### Limits

Three static limits protect the compiler:

| Limit | Value |
|---|---|
| Body size | 32 KiB |
| Recursion | forbidden — a cycle is a compile error |
| Expansion depth | 16 nested macros |

A recursive macro fails at declaration time:

```
@macro loop() { @loop() }
  error: @macro loop: recursion cycle loop -> loop
```

Expansion depth is bounded at parse time:

```
error: macro @m5: expansion depth exceeded (16)
```

### Error format

A syntax error inside a macro body reports three things: the actual
file line of the failing token, the macro's name, and the macro's
definition line.

```
test.nflow:3: in macro @broken (defined at test.nflow:2): unexpected token ")"
```

Nested macros chain:

```
test.nflow:9: in macro @outer (defined at test.nflow:5):
  test.nflow:2: in macro @inner (defined at test.nflow:1): unexpected token ")"
```

Both file:line positions are clickable in modern terminals.

### Hygiene

Macros are hygienic by construction. `$param` substitution is textual,
but the resulting AST enters the caller's pipeline without introducing
locals, bindings, or names — a macro cannot see or mutate the caller's
state outside the pipeline value it receives. This is not enforced by a
check; it is a property of the expansion model.

---

## 12. `@require` — external bundles

```nflow
@require github.com/nexssp/flow/extensions/macros
@require ./local/helpers { prefix: "DEMO" }
@require github.com/example/fancy v1.2.3 as fancy
```

The CLI builds or reuses a harness binary that links the requested
modules. A local path (`./helpers`) works if the directory has a `go.mod`
ancestor or contains Go files directly. Options passed in `{ ... }` are
validated by the bundle: an unrecognized key is a compile error.

---

## 13. Modifiers you'll use

The standard Kernel policy modifiers, available on any action that
supports them:

```nflow
api.call:timeout=5s:retry=3:cache=1m
```

| Modifier | Value | Effect |
|---|---|---|
| `:timeout=5s` | duration | Deadline for one call |
| `:retry=3` | int | Retry transient failures up to 3 times |
| `:cache=30s` | duration | Cache the result for 30 seconds |
| `:dedup` | flag | Collapse concurrent identical calls |
| `:coalesce` | flag | Share one in-flight call with all waiters |
| `:rate_limit=100` | int | Token-bucket admission control |
| `:concurrency=8` | int | Bound simultaneous invocations |
| `:idempotent` | flag | Idempotency-key middleware |

A bundle can define its own modifiers for its own actions; those are
listed by `nflow list`.

---

## 14. Inspecting a flow before running it

```sh
nflow lint    flow.nflow          # static check, exits 1 on any issue
nflow explain flow.nflow          # effective policies per atom
nflow info    flow.nflow          # pipeline shape
```

`nflow explain` shows what the compiler sees, with source attribution:

```
flow.nflow

  @pipeline fetch  [definition]
    pipeline.fetch
      :tag=api             from pipeline fetch
    http.request
      :timeout=5s          from profile reliable
      :retry=3             from profile reliable

  [top-level]
  pipeline.fetch  [invocation]
    (no modifiers)
```

Each modifier lists the scope, profile, or pipeline that produced it.

---

## 15. Known limitations

- **Outer `@scope` does not reach into `@pipeline` bodies.** A
  `@scope` wrapping a `@pipeline` declaration only affects atoms at
  the top level. Scope inside a `@pipeline` body works as expected.
- **Profiles must be declared before use.** No forward references.
- **Macro bodies cannot reference names declared only by their
  caller.** A macro sees the top-level registry, not a caller-local
  binding.
- **Stream sources and operators ignore inheritable policy modifiers.**
  A `:timeout` on `fs.walk` has no meaning; stream semantics are
  governed by the source's own config.
- **`nflow explain` does not show `:parent=` ancestry.** A child
  profile's own modifiers are shown; the parent's are shown with the
  parent's label.
