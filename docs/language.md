# `.nflow` language

A `.nflow` file contains Flow directives and a pipeline expression. An atom names an action available to the runner; operators compose actions, and projection expressions can reshape values between them. The examples linked here are current runnable fixtures.

## Pipeline syntax

| Form | Meaning |
|---|---|
| `A -> B` | Pass the left result to the next action. |
| `A | B` | Syntax sugar for `A -> B`; it does not introduce an action alias. |
| `A || B` | Try the right expression if the left expression fails. |
| `(A & B)` | Run the grouped branches in parallel. |
| `{ key: expression }` | Project fields and expressions into a new object. |
| `action @{ key: value }` | Pass named values as the action's arguments. |
| `action:timeout=1s` | Apply a modifier to that action. |

The binary operators and their current descriptions are also available from `nflow list`. The built-in feature suite tests parallel composition with this expression:

```nflow
( const @{ value: "left" } & const @{ value: "right" } )
```

Parallel results use each branch action's canonical name as its gather key. A name used by only one branch keeps its existing key; repeated names get `name#1`, `name#2`, and so on in source order (for example, `const#1` and `const#2`). If a generated label is already in use, trailing `#` characters are added until it is unique. The checked [parallel actions example](../examples/00_flow_basics/07_parallel_actions.nflow) demonstrates this mapping with local actions and asserts the resulting map. For a verified end-to-end projection, see [data transformation](../examples/00_flow_basics/04_data_transformation.nflow). For a working timeout and fallback flow, see [sleep and timeout](../examples/00_flow_basics/02_sleep_and_timeout.nflow).

## Native keywords

Native keywords are grammar-level conveniences. They compile to canonical
action IDs at parse time and are not registry aliases: no bundle can
extend or override them.

| Keyword | Canonical target   |
|---------|--------------------|
| `const` | `runtime.const`    |
| `with`  | `runtime.with`     |
| `pick`  | `runtime.pick`     |
| `wrap`  | `runtime.wrap`     |
| `fail`  | `runtime.fail`     |
| `noop`  | `runtime.noop`     |
| `sleep` | `runtime.sleep`    |
| `env`   | `runtime.env`      |
| `uuid`  | `runtime.uuid`     |
| `json`  | `json.clean`       |

Both forms are valid. Prefer the keyword in new code.

Inside string values (`@{ action: "runtime.noop" }`, `@pool [...]`) the
canonical form is still required: those positions are runtime values, not
grammar. Phase 2 introduces `CapabilityRef` to close that gap.

## Directives

Directives are handled before the pipeline is compiled. Common directives in the current compiler include:

| Directive | Use |
|---|---|
| `@description "..."` | Attach a human-readable description. |
| `@assert: expression` | Check the final result after the flow runs. |
| `@pipeline name` … `@end` | Declare a named pipeline. |
| `@include "path.nflow"` | Include another local Flow file. |
| `@require ...` | Request an extension or library bundle for the flow; optional `as name` replaces its namespace locally. |

For example, current example files use `@assert: result.summary == "Ada (ID: 101) is active"` to check their result. Directive and modifier availability can depend on the bundles loaded by a runner; use `nflow list` to inspect the active catalog rather than relying on a fixed feature list.

Actions, stream sources, and stream operators have one canonical fully-qualified name, such as `const`, `fs.walk`, or `render.markdown`; extension libraries do not publish short-name synonyms. An explicit `@require ... as local` changes the first namespace segment to `local` for that flow. This local qualifier is the only Flow alias mechanism. The `|` character remains pipeline syntax sugar for `->`, not an action alias.

## Policy inheritance

Two directives govern inherited policy:

- `@scope :mods { ... }` — anonymous; applies to atoms inside the block.
- `@profile NAME :mods` — named; reused via `:profile=NAME` on `@pipeline` or `@scope`.

### Precedence

From highest priority to lowest:

1. Atom-local modifier (`atom:mod`)
2. Innermost `@scope`
3. Outer `@scope`
4. `@pipeline` local modifiers
5. `@pipeline :profile=NAME`
6. Kernel defaults

More local wins. Later-declared (inner) scope spans override earlier (outer) spans on the same modifier name.

### Explicit disable

An atom-local `:retry=0` is an explicit disable. It overrides any inherited `:retry=N` from scope or profile. `:retry=0` means "off"; absent means "inherit".

### What inherits

Only policy modifiers propagate from `@scope`/`@profile` to atoms:

```
:timeout  :retry  :cache  :dedup  :coalesce  :rate_limit  :concurrency  :idempotent  :breaker
```

Metadata modifiers — `:tag`, `:status`, `:route`, `:name`, `:scope`, `:desc` — stay on the atom or `@pipeline` wrapper where they are declared. They do not inherit.

### Known limitations

- Top-level `@scope` does not reach into a `@pipeline` body. Scope inside a pipeline only affects atoms in that pipeline.
- A profile must be declared before its first use in the file. No forward references.
- Inherited policy modifiers on stream sources and operators are silently ignored (the source's config struct is authoritative). Dedicated stream semantics are not yet implemented.
- `nflow lint` does not yet apply scope inheritance.

## Next steps

See the [CLI guide](cli.md) for run, lint, build, and catalog commands, or the [extension guide](../extensions/README.md) for how bundles add compiler features.
