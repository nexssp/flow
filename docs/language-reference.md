# Flow language and runtime reference

This is the implementation-backed reference for `.nflow` source in this Flow checkout. It describes the parser, compiler, runner, standard CLI bundle set, and extension-dependent features as implemented—not a proposed grammar. The exact capabilities visible to a flow depend on the bundles mounted by its host. The shipped CLI's standard set is defined centrally in [`native/native.go`](../native/native.go#L68-L124); `@require` and host code can add further extensions.

Use `nflow list --flow FILE`, `nflow show NAME --flow FILE`, or `nflow catalog --flow FILE` to inspect the live registry for a particular source. This matters: an extension directory in the repository does not by itself make that extension available to every host.

## How a Flow runs

A `.nflow` file is preprocessed first (directives, included sources, constants and bundle requirements), then parsed using the active operator and keyword-primary registries, then compiled against the active action and modifier registries. Compilation resolves action names and declared capability references; execution evaluates projections, applies runtime policies, invokes actions, and validates values at runtime where the feature requires it. The compiler and its extension tables are in [`compiler/`](../compiler/); standard CLI registration is in [`native/native.go`](../native/native.go).

The syntax below is a composition language for registered actions. It is not a promise that every arbitrary identifier names an action or that every expression is checked against external service data before a run.

## Actions, arguments, and data

An action atom is normally a canonical action name, optionally followed by modifiers and a structured argument block:

```nflow
orders.fetch :timeout=3s @{ order_id: .order_id }
```

`@{ ... }` is a data-value block: it accepts strings, numbers, booleans, arrays, nested objects, the current input (`.`), and paths into it (`.order_id`, `.customer.id`). It is not itself an arbitrary expression block. Use a projection or a condition when calculation is needed. An argument may also contain a bare capability reference only when the action's declared argument schema identifies that field as a capability; the compiler resolves that reference against the active registry. Unknown action names, invalid registered modifiers, and unresolved declared capabilities are compile-time errors. For the introductory syntax walkthrough, see [`docs/language.md`](language.md); see also the [argument schema/compiler tests](../compiler/arg_schema.go).

The older `action(key=value, ...)` parameter spelling is still parsed for compatibility; prefer `@{ ... }` for structured input. Action argument details are assembled in [`compiler/parser.go`](../compiler/parser.go#L339-L500) and [`compiler/ast_build.go`](../compiler/ast_build.go).

Flow also has ten reserved keyword actions that map to canonical capabilities and cannot be shadowed by a bundle:

| Keyword | Canonical action |
|---|---|
| `const` | `runtime.const` |
| `with` | `runtime.with` |
| `pick` | `runtime.pick` |
| `wrap` | `runtime.wrap` |
| `fail` | `runtime.fail` |
| `noop` | `runtime.noop` |
| `sleep` | `runtime.sleep` |
| `env` | `runtime.env` |
| `uuid` | `runtime.uuid` |
| `json` | `json.clean` |

`const` here is a runtime action; it is distinct from the compile-time `@const` directive. The keyword map is in [`compiler/reserved.go`](../compiler/reserved.go) and the action fixtures are under [`extensions/runtime/nflows/`](../extensions/runtime/nflows/).

Atoms can also carry extension-consumed annotations: `#target1,target2`, `~excluded1,excluded2`, and `@prompt` add target, exclusion, or prompt data to the action request when the action supports it. `@config` on an atom is a special configuration-injection marker for configurable stream sources/operators; it is not the same as the `@config` directive. These annotations have no universal effect independent of the consuming action; see the parser's [`Atom` fields](../compiler/ast.go#L35-L49) and builder [argument assembly](../compiler/ast_build.go#L840-L875).

## Expressions, projections, and symbols

A projection transforms the current value into a new object. Inside it, `.field` means a field on the current input; `...` spreads the current object before the listed replacements:

```nflow
{ id: .user.id, display_name: .user.name }
{ ..., status: "ready" }
```

Projection expressions and runtime condition expressions are evaluated using `expr-lang`, with Flow's leading-dot input notation rewritten for the evaluator. For example, supported fixtures use arithmetic, comparisons, boolean operators, object construction, and expression-library functions such as `filter` and `sortBy`; the accepted functions depend on the evaluator environment. Do not confuse this expression language with the literal/action-argument parser. Projection implementation and fixtures: [`extensions/projection/`](../extensions/projection/); expression preprocessing: [`compiler/expr.go`](../compiler/expr.go).

Payload field names do not need to avoid expression builtins: `.count > 4`, `.type == "invoice"`, `.len > 0`, and `.min < 10` refer to input fields, while `count(items, # > 0)`, `type(value)`, `len(items)`, and `min(3, 8)` remain function calls. Ordinary paths keep their established rewrites (`.name` to `name`, `.user.name` to `user.name`); only leading fields that collide with the installed expr builtin registry, expression keywords, or Flow's `__root__` binding are root-qualified. A later segment such as `.user.count` remains a member lookup. Quote keyword object keys when necessary, for example `{ "and": 7 } -> { value: .and }`. See the [exhaustive builtin regression tests](../compiler/preprocess_collisions_test.go) and [executable fixture](../extensions/projection/nflows/reserved_fields.nflow).

The standard projection bundle validates statically known projection bodies during compilation, so `nflow lint` can reject malformed expressions before actions run. Loop, assertion, match and error-handler conditions also compile their expressions before execution. If a failed expression actually resolves a bare name such as `count` to a builtin value, its diagnostic suggests `.count` for payload access. This is a hint on an existing compile failure, not a blanket ban on builtin names or function values. Dynamic payload types, missing fields and projection bodies supplied dynamically to an action still require runtime checks; compilation cannot prove all data-dependent behavior.

`@symbols NAME { ... }` declares a file-local namespace of string values. In expression positions, refer to them as `NAME.member`; in data positions, an unquoted known symbol resolves to its declared string. Local namespaces shadow same-named global namespaces; current input values take precedence on collisions in expression environments. An included module's public namespace is addressed through its import qualifier, for example `billing.queues.review`; its unqualified declarations remain local to that module. See [`docs/symbols.md`](symbols.md) and [`extensions/symbols/`](../extensions/symbols/).

## Pipeline operators and control flow

The core parser obtains operators from the active table. In the shipped standard set, the supported pipeline operators include:

| Form | Meaning |
|---|---|
| `left -> right` | Pass the left result as the right action's input. |
| `left | right` | Pipeline alias with the same dataflow intent. |
| `left || right` | Run the right side if the left side returns an error; it catches errors generally, not only one named error kind. |
| `(left & right)` | Run branches concurrently and gather their results. Repeated canonical action names receive numbered result keys. |
| `parallel { label: expr, ... }` | Run labeled branches concurrently and return their results by label. Labels must be unique and nonempty. |
| `race { expr, ... }` | Return the first successful candidate; cancel outstanding work after a winner. If all candidates fail, the race fails. |
| `condition ? yes : no` | Conditional expression; the else arm is optional. |

Operator parsing is registry-driven: consult [`extensions/syntax/`](../extensions/syntax/) and [`compiler/operator.go`](../compiler/operator.go). The `parallel` and `race` keywords are supplied by the standard `syntax` bundle, not by a bare parser with no extensions. Parallel branch failure propagates without a partial result map. A race may take longer to return if a launched branch does not cooperate with cancellation. Further syntax discussion is in [`docs/language.md`](language.md) and exact fixtures are in [`extensions/syntax/nflows/`](../extensions/syntax/nflows/).

`match` chooses the first matching arm. It accepts predicate arms against the current input, or a subject expression with literal/pattern arms; subject-relative relational arms (`>`, `>=`, `<`, `<=`) compare the evaluated subject with the arm expression. `_` is the default arm:

```nflow
match {
  .danger && .level > 3 -> const @{ value: "critical" },
  .danger               -> const @{ value: "warning" },
  _                     -> const @{ value: "nominal" }
}
```

The example above is the executable [`match_predicates.nflow`](../extensions/match/nflows/match_predicates.nflow) fixture. Predicate evaluation errors are skipped while scanning for a matching arm; see [`extensions/match/conditions.go`](../extensions/match/conditions.go). Subject-relative example: `match(.peak) { > 90 -> "HIGH", _ -> "NORMAL" }`.

`loop(body) until(condition)` is also an extension-provided keyword. Its condition is evaluated against the value produced by the body, and the compiled loop is bounded to 15 iterations; it is not an unbounded `while` loop. Example: [`extensions/loop/nflows/basic.nflow`](../extensions/loop/nflows/basic.nflow), implementation [`compiler/ast_build.go`](../compiler/ast_build.go#L644-L670).

### Error routing and assertions

There are two runtime assertion forms:

- `assert(condition, message)` is a runtime expression in the pipeline. It checks the current value and fails that execution if the condition is false.
- `@assert: expression` records a post-run assertion evaluated against the completed result (including `result`, direct result fields, `duration_ms`, and `actions`). The CLI reports failures and exits nonzero. `nflow run --assert=EXPR` adds the same kind of post-run check from the command line.

For frequent checks, `@assert {` starts a multiline block with one assertion per non-empty, non-comment line, and `@assert: { expr1, expr2, ... }` is a compact list. Each expression is evaluated independently in source order; commas inside strings, nested calls, or collections do not split the compact list. Expressions use the existing expr-lang evaluator (for example, `len(result.items) > 0` or `all(result.items, # != "")`). Failure output identifies the assertion's source file and line. The original single-expression form is unchanged.

These are runtime checks; neither is a compile-time input schema. See [`extensions/assert/`](../extensions/assert/), [`runner/assertions.go`](../runner/assertions.go), and [`extensions/assert/nflows/`](../extensions/assert/nflows/).

For recovery, `? on_error ...` wraps one expression and provides an error environment to its handler; `@on_error ...` wraps a whole pipeline and invokes its handler with the original request. Block forms can route by error kind; short forms select one target. Without a matching case/default, the error propagates. Neither form recovers standard caller cancellation. `||` is separate and catches a failing left branch. See [`docs/on-error.md`](on-error.md) and [`extensions/on_error/`](../extensions/on_error/).

`@approval NAME { role: ..., reason: ..., expires: ... }` declares a runtime approval gate used as `approval.NAME`. The gate suspends unless the run context contains a matching token; durable pause/resume requires a suspension-capable journal. The CLI's file journal supports this workflow, but a token is only a gate-name match—not identity verification. See [`docs/approval-resume.md`](approval-resume.md) and [`extensions/approval/`](../extensions/approval/).

## Directives and declarations

Directives are preprocessed declarations, not ordinary action calls. The stock CLI's registered directive handlers come from the bundles in [`native/native.go`](../native/native.go#L84-L124). The following list is the stock language surface in this checkout; extension bundles may add their own directives.

| Directive | Form and effect |
|---|---|
| `@description` | `@description "text"`; descriptive metadata. |
| `@assert` | `@assert: result.ok == true`, `@assert { ... }`, or `@assert: { expr1, expr2 }`; post-run assertions, evaluated after execution in source order. |
| `@pipeline` / `@end` | `@pipeline NAME` followed by header modifiers and a Flow expression, then `@end`; declares callable `pipeline.NAME`. Header modifiers may be same-line or on indented colon-only lines. `@end` alone is an error. |
| `@scope` | `@scope :timeout=5s :retry=3 {` newline, scoped source, then `}`; applies inheritable modifiers within its source span. An atom's own modifier wins; nested scopes override by name. |
| `@profile` | `@profile reliable` with same-line or indented colon-only modifier lines; reusable policy set, optionally inheriting `:parent=NAME`. Define it before use; unknown/cyclic profiles fail compilation. |
| `@macro` | `@macro greet(name) { ... }`, invoked as `@greet("value")`; compile-time source substitution. Duplicate names, recursive definitions, bodies above 32 KiB, and expansion nesting beyond 16 are rejected. |
| `@require` | `@require MODULE [VERSION] as ALIAS` with an optional options block; loads a registered Go extension bundle. Exact module/version/options support depends on the loader and bundle. |
| `@include` | `@include "relative/path.nflow" [AS namespace]` imports a local source module; `@include alias:path/in/bundle.nflow [AS namespace]` imports an exported module from an `@require` bundle. |
| `@config` | `@config:key=value` or a block such as `@config { strict: true }`; compile/runtime configuration strings. `strict=true` enables runtime request validation on typed actions unless overridden by `:lenient`. |
| `@config.load` | `@config.load:path="settings.json" [:prefix="app"] [:required=false]`; loads configuration at preprocess time and flattens nested keys into the compile-time config map. Relative paths use the source base directory. JSON and a limited TOML subset work in the stock bundle; YAML needs the optional `config_yaml` bundle. |
| `@const` / `@const.load` | `@const NAME = VALUE` substitutes compile-time constants. `@const.load "settings.json" as settings [:schema=Settings]` loads JSON into a constant namespace and can validate it against a previously declared schema during preprocessing. |
| `@nflow_version` | `@nflow_version ">=0.37.0 <0.40.0"`; compile-time compiler-version constraint on `MAJOR.MINOR.PATCH`. Whitespace-separated terms are combined with AND: every comparison must hold. Supports `=`, `==`, `>=`, `<=`, `>`, `<`, or a bare equality version, including a space after an operator. An optional `v` prefix is accepted and pre-release/build suffixes are ignored for comparison. No OR, wildcard, caret, tilde, or hyphen range syntax. Development/unknown compiler builds retain the existing bypass of constraint validation and comparison. |
| `@symbols` | `@symbols NAME { key: "value" }`; file-local expression/data namespace. |
| `@schema` | `@schema NAME { fields }`; declares a runtime payload schema. Field syntax and tags are shown below. |
| `@hook` | `@hook:hook.verify,observe.metrics`; attaches registered Kernel hooks to the compiled program. Names must exist in the active Kernel registry or materialization fails. |
| `@pool` | `@pool workers [action.a, action.b] { strategy: "failover" }`; declares a dispatcher `pool.workers`. Strategies are `round_robin` (default), `failover`, and `hash`; `hash` requires `key: ".field.path"`. |
| `@on` | `@on event "nats:orders.incoming"`; records an event trigger declaration. This metadata does **not** mean the stock CLI currently starts a listener; see limitations below. |
| `@approval` | `@approval review { role: reviewer, reason: confirm, expires: 1h }`; declares an approval gate. |
| `@on_error` | `@on_error TARGET` or a block with `when condition -> target` and optional `else -> target`; routes runtime failures. It does not recover standard caller cancellation. |

For a required source bundle, the qualifier before `:` is its local `@require` alias, while `AS` still controls the included module's namespace. For example, `@require github.com/nexssp/si-ai v1.2.3 as ai` followed by `@include ai:nflows/lib/critic.nflow AS critic` imports the critic pipeline as `critic.pipeline.*`. Bundle modules must be listed explicitly in `compiler.Bundle.ExportedModules`; see [Bundle source modules](bundle-source-modules.md) for the API and privacy rules.

### Multiline header modifiers

```nflow
@profile reliable
  :timeout=2s
  # blank lines and comment-only lines are trivia
  :retry=4
@pipeline fetch_user
  :profile=reliable
  :tag="api"
  http.request @{ url: "https://api.example/users/1" }
@end
```

A continuation line must begin with indentation (spaces/tabs) deeper than the directive line and contain only one or more modifiers. Blank lines and whole-line `#`/`//` comments may separate header lines. After the final modifier, the first other significant line permanently begins the body—even a macro invocation; later colon-prefixed lines belong to the body. Trivia after the final modifier also belongs to the body, not the header. Same-line modifiers remain valid. Modifier values may be flags or values, compact or spaced, and support single-, double-, and backtick-quoted strings with escapes; unquoted values stop at colon, whitespace, `#`, or `//` (quote URLs/routes containing these characters). No source rewriting/folding is done. A modifier-only header that is malformed reports an error at its source line.

`@include` is a source-based module import, not textual expansion or a metadata merge. `@include "payments.nflow" AS billing` exposes exported pipelines and capabilities under `billing.pipeline.*` and `billing.*`; without `AS`, the filename stem is the namespace (so `payments.nflow` is available under `payments.*`). An imported clean body is retained as the compiled module body and invoked at its import site; declaration-only imports add no execution atom by themselves. Each module has its own declaration environment and resolver: child pipelines resolve their local pipeline/symbol references locally, while the parent keeps its own declarations. Nested imports compose their own qualifiers. Duplicate declarations are compile errors that identify both the duplicate and original source locations. `@require` remains a separate extension-loading directive and is not affected by module imports.

Constants, profiles, macros and schemas are local to their declaring module. Imported capabilities and qualified symbols are exported; imported macros and schemas are not available in a parent's modifiers or declarations. Directive syntax and behavior are implemented in [`extensions/`](../extensions/) by the corresponding directive handlers; tests and embedded `.nflow` fixtures under each extension directory are the accepted examples. The introductory walkthrough of reusable pipelines and macros is in [`docs/language.md`](language.md).

The compiler-version directive is spelled `@nflow_version` only; the previous spelling is no longer registered. This is a breaking syntax change. The extension package remains `extensions/flow_version`.

`@config.load` and `@const.load` are different: the former writes flattened values to compile-time configuration; the latter creates a named JSON symbol and can validate it against `@schema`. YAML loading via `@config.load` requires the optional `config_yaml` bundle to be added with `@require` or mounted by the host. See [`extensions/config/load.go`](../extensions/config/load.go), [`extensions/config/load_test.go`](../extensions/config/load_test.go), [`extensions/flow_version/library.go`](../extensions/flow_version/library.go), and [`extensions/flow_version/library_test.go`](../extensions/flow_version/library_test.go).

### Extension-dependent directive

`@embed NAME "relative/path"` is supplied by the optional `embed` bundle, not by `native.Bundles()`. The bundle must be mounted with an explicit filesystem root (there is no implicit process-working-directory root); it validates the path, applies a byte limit (16 MiB by default), and exposes the asset reference as `embed.NAME`. The asset bytes remain behind the file resolver rather than being copied into Flow metadata. See [`extensions/embed/library.go`](../extensions/embed/library.go) and [`docs/static-embed.md`](static-embed.md).

## Schema declarations and validation boundaries

`@schema` is implemented and is the right construct for declarative input shape/validation. The accepted declaration forms are `@schema NAME { ... }` and `@schema NAME struct { ... }`. Fields use `Field Type` and optional Go-style backtick tags; embedded schemas must be declared earlier. Supported scalar categories include strings, signed/unsigned integers, floats, booleans, and `any`/`interface{}`; slices, maps, pointers and struct-shaped values are recognized by the schema parser. Field names default to a lower-cased initial letter for their JSON key; `json:"..."` overrides it. Declaration syntax, names, tag syntax, defaults, duplicate names and references to unknown schemas are checked before a run.

```nflow
@schema Request {
  Name  string `json:"name" default:"standard" validate:"required"`
  Depth int    `json:"depth" default:"1" validate:"gte=1"`
}

@pipeline handle
  :request-schema=Request
  noop
@end
```

This declaration/header pattern is exercised by [`extensions/schema/nflows/pipeline_boundary.nflow`](../extensions/schema/nflows/pipeline_boundary.nflow). At invocation, `:request-schema=NAME` applies defaults and validates the incoming payload before the first action. `:response-schema=NAME` validates the returned value without applying defaults and returns that exact value unchanged; response schemas containing any `default` tag are rejected before execution. Prefix either selected name with `!` to independently reject undeclared JSON-visible fields at that boundary. The compiler checks declarations and schema references; it cannot validate a future request or response payload at compile time. Runtime behavior is tested in [`runner/schema_boundary_test.go`](../runner/schema_boundary_test.go) and implemented in [`extensions/pipeline/materialize.go`](../extensions/pipeline/materialize.go).

Important schema rules:

- Fields are not automatically required just because they appear in a schema. Use a `validate:"required"` rule when requiredness is intended; validator rules such as `email`, `gte=1`, `oneof=...`, and `dive,required` are delegated to the installed validator.
- `required` uses the validator's non-zero-value semantics for scalar types, so it rejects values such as `0`, `false`, or `""`, not merely an absent key. Account for that when zero/false is a valid business value.
- Defaults apply only when a key is absent. Explicit `null`, zero, `false`, or an empty string is not replaced; composite defaults replace the whole value rather than deep-merging.
- Extra input keys are allowed and preserved unless that request or response boundary uses the `!` strictness marker.
- Pipeline boundaries use separate `:request-schema=NAME` and `:response-schema=NAME` modifiers; the former pipeline-header alias `:schema=NAME` is removed. A direct `NAME.validate` action provides an explicit mid-pipeline validation step.
- The atom modifier `:schema=NAME` by itself only adds a `schema:NAME` metadata tag; it does not insert a validation action or validate that atom's request. Do not substitute it for a pipeline header or `NAME.validate`; see [`SchemaModifier`](../extensions/schema/library.go#L73-L79).
- `@const.load ... :schema=Name` validates loaded compile-time JSON during preprocessing, not a future runtime request.

The implementation proof is [`extensions/schema/directive.go`](../extensions/schema/directive.go), schema materialization and the `NAME.validate` action are in [`extensions/schema/materialize.go`](../extensions/schema/materialize.go) and [`extensions/schema/action.go`](../extensions/schema/action.go), and pipeline boundary application is in [`extensions/pipeline/materialize.go`](../extensions/pipeline/materialize.go). Full tag/default behavior is in [`docs/schema.md`](schema.md).

A separate runtime strict mode is also available: `@config:strict=true` or `:strict` attaches the validation hook to typed action requests; `:lenient` suppresses it for that atom. This validates typed action request structs that carry validation tags. It is runtime request validation, not static payload checking and not a replacement for a named pipeline input schema. See [`compiler/strict.go`](../compiler/strict.go), [`compiler/ast_build.go`](../compiler/ast_build.go#L35-L55), and [`compiler/strict_test.go`](../compiler/strict_test.go).

## Modifiers and policy

An atom can carry modifiers as `action:flag` or `action:key=value`; multiple modifiers may be written together: `service.call:timeout=5s:retry=3`. They can decorate atoms, parenthesized expressions, projections, or supported keyword expressions. Each modifier name/value is interpreted by the active modifier table and extension advisors. Unknown names, invalid value types, and invalid registered values are compile-time errors; runtime effects belong to the Kernel action builder or extension.

Flags and valued modifiers may be mixed on one line, for example `runtime.const :rate_limit=50 :idempotent :dedup :coalesce @{ value: "ok" }`. A colon immediately followed by a modifier name starts the next modifier whether or not that name has `=value`; it is not part of the preceding value. Quoted strings preserve spaces, commas and colons, for example `:desc="Maks: ingest, Julia: verify"`. The existing ternary convention is unchanged: a separator colon followed by whitespace belongs to `condition ? yes : no`, not to a modifier. This is a boundary fix, not a new atomic-scalar-only grammar; existing comma-valued modifiers remain supported. See [`compiler/parser_modifier_boundary_test.go`](../compiler/parser_modifier_boundary_test.go).

The stock CLI includes these principal groups:

- Kernel policy: `timeout`, `retry`, `concurrency`, `cache`, `coalesce`, `dedup`, `idempotent`, `rate_limit`.
- Identity guards: `auth`, `role`, `perm`, `feature`.
- Metadata: `name`, `desc`/`description`, `status`, `tag`, `scope`, `read_only`, `audit`, `debug`, `deprecated`, `strict`, `lenient`.
- Schema extension: `schema` (see the distinction above).
- Optional, non-stock resilience extension: `circuit_breaker`. When mounted, its adviser also consumes selected Kernel policy modifiers; see [`docs/resilience.md`](resilience.md).

The actual stock modifier contract is in [`extensions/modifiers_core/modifiers.go`](../extensions/modifiers_core/modifiers.go), [`extensions/modifiers_auth/modifiers.go`](../extensions/modifiers_auth/modifiers.go), [`extensions/modifiers_meta/modifiers.go`](../extensions/modifiers_meta/modifiers.go), and [`compiler/modifier.go`](../compiler/modifier.go). Retry and resilience behavior is in [`docs/retry.md`](retry.md) and [`docs/resilience.md`](resilience.md); resilience is not in the ordinary `native.Bundles()` set in this checkout.

Reusable policies are applied by scopes and profiles. Atom-local settings take precedence over inherited settings; local pipeline-header modifiers override profile-provided modifiers of the same name. Use `nflow explain FILE --ast` to inspect effective modifiers and their origins.

Profile modifiers may appear on the declaration line, for example `@profile reliable :timeout=2s :retry=4`, or on indented modifier-only continuation lines. Both declaration headers and action modifiers support readable multiline formatting; the first body expression ends declaration-header collection. The original master declaration prefix and the runnable multiline example are checked in the [master conformance tests](../runner/master_conformance_test.go).

## Bundles, extensions, and hooks

The standard CLI bundle set includes core syntax/runtime, projections, symbols, match, loops, assertions, schema, pipelines, scopes, profiles, macros, include/require, pools, approval, error handling, retry, stream operators and other registered action libraries. The exact source of truth is [`native.Bundles()`](../native/native.go#L84-L124). `native.SelftestBundles()` adds fixtures/test-only or optional in-tree bundles; that does not make them available to ordinary flows. For example, `config_yaml`, `decide`, `resilience`, and `template` are only in the self-test bundle list here. Polyglot language keywords are provided by the standalone `github.com/nexssp/lang/nexssflow` adapter and loaded through external `@require`; they are not part of Flow's native bundle set. The `embed` extension requires a host/bundle that mounts it. See [`native/native.go`](../native/native.go#L127-L157), [`docs/static-embed.md`](static-embed.md), and [`docs/template.md`](template.md).

`@require` makes extension capabilities explicit for the CLI's build harness; a custom Go host may mount bundles directly. Bundles can contribute actions, directives, modifiers, operators, keyword primaries, symbols, hooks, preprocessors and materializers. A directive or operator supplied by an extension is valid only when that extension is active. The extension contract is described in [`docs/extensions.md`](extensions.md).

`@hook` accepts one or more comma-separated hook names. The directive records them; the hook bundle resolves names against the Kernel registry and attaches them to the compiled program. Unknown names fail rather than silently becoming no-ops. The built-in `hook.verify` is a test/example hook, not a universal application hook. See [`extensions/hook/directive.go`](../extensions/hook/directive.go) and [`extensions/hook/library.go`](../extensions/hook/library.go#L55-L73).

## Compile-time checks versus runtime behavior

| Checked before execution | Checked or performed at execution |
|---|---|
| Preprocessing and parse errors; malformed directives; duplicate declarations; macro cycle/size/depth limits; `@nflow_version` comparison and compile-time `@include`/`@config.load` file loading. | Action calls and external effects; projections and conditions against current values; action failures and error routing. |
| Action, operator, keyword-primary and modifier resolution against active registries; declared capability-reference resolution; response-schema default declarations are rejected. | Pipeline request defaults and validation plus response validation via `:request-schema=NAME` and `:response-schema=NAME`; `!` independently rejects extra fields at either boundary; `NAME.validate`; `@assert`, `assert(...)`, and command-line `--assert`. |
| Schema declaration/tag/default syntax and declared schema-name references; `@const.load` schema checks and opt-in `@embed` asset/path checks. | Strict typed-request hooks; timeout/retry/concurrency/cache/idempotency policies; approval pause/resume; parallel/race cancellation. |
| Bundle requirement parsing and extension option validation where the bundle declares its accepted options. | Remote-service availability, credentials, payload semantics beyond the active schema, and host-specific capability behavior. |

`nflow lint` checks source against the live registry without executing the Flow. A successful lint does not prove that a payload will pass runtime schema validation, that a service is available, or that a side effect will succeed. Likewise, schema declarations are compile-time checked but request values are validated only when the pipeline is invoked.

## CLI invocation

Typical use:

```sh
nflow run path/to/flow.nflow '{"order_id":"A-42"}'
nflow lint path/to/flow.nflow
nflow explain path/to/flow.nflow --ast
nflow list --flow path/to/flow.nflow
```

`nflow run FILE [JSON_PAYLOAD]` executes a Flow; command help lists verbosity, post-run assertions, info-only mode, journal/resume, and approval flags. The shipped CLI also exposes `init`, `build`, `serve`, `lint`, `explain`, `expand`, `catalog`, `list`, `show`, `completion`, `require`, `pin`, `info`, `self test`, and version/help commands. See [`docs/cli.md`](cli.md) and the command definitions in [`cli/help.go`](../cli/help.go).

### Explicitly unsupported or limited in this checkout

- `nflow serve` parses and compiles `@on event` flows, performs safety checks, and waits for shutdown, but **the listener is not implemented**. It is not a production event service. This limitation is stated by [`cli/serve.go`](../cli/serve.go#L83-L93) and CLI help [`cli/help.go`](../cli/help.go#L54-L60).
- `@on event "protocol:target"` records trigger metadata; do not infer a running listener from that declaration. The supported trigger declaration form is tested in [`extensions/on/library_test.go`](../extensions/on/library_test.go).
- There is no general `if/else` block or arbitrary unbounded `while` construct in this grammar. Use the implemented ternary, `match`, bounded `loop`, `||`, or error-routing forms described above.
- The language is registry-driven: a syntax feature found in a non-default extension is not universally available. Check the active registry instead of inferring availability from a package directory or documentation example.
- The stock schema feature does not statically type-check future request payloads, reject unknown keys, or enforce application-specific semantics that are not declared as validation tags. Keep those checks in the appropriate runtime action or business rule.

Flow `parallel`, `race`, and `match` expressions may also appear as projection property values. They remain real Flow AST children: they are analyzed and built through the active registry, and each receives the projection’s incoming root as input. Sibling values do not form a sequential dependency chain. The surrounding object expression is still evaluated by expr-lang, so conditional branches remain lazy; control-level `parallel`/`race` use Kernel cancellation and joining, while ordinary object fields are not implicitly concurrent. Typed errors and cancellation propagate. An expr-lang iterator item (for example `#` in `map` or `filter`) is not automatically rebound as the Flow control input: controls use the projection root. This does not make `@{ ... }` an arbitrary interpolation/expression block. See the [master conformance audit](master-conformance.md).

## Source and conformance map

The most direct proof for the key contracts is in the implementation and executable tests linked above. Particularly useful entry points are [`compiler/parser.go`](../compiler/parser.go), [`compiler/compile.go`](../compiler/compile.go), [`compiler/operator.go`](../compiler/operator.go), [`extensions/schema/directive_test.go`](../extensions/schema/directive_test.go), [`runner/schema_boundary_test.go`](../runner/schema_boundary_test.go), and the embedded `.nflow` fixtures under each extension. The introductory walkthrough remains in [`language.md`](language.md); detailed schema, error, resilience, approval, extension, CLI and symbol behavior is linked throughout this reference.

[`runner/testdata/master_supported.nflow`](../runner/testdata/master_supported.nflow) is runnable with the stock CLI and carries ten post-run assertions; [`runner/master_conformance_test.go`](../runner/master_conformance_test.go) executes it through the native runner, checks its declared assertions, and separately invokes its resilience pipeline with an explicit host idempotency store. The original master is retained unchanged as full end-to-end conformance coverage; the separate supported fixture remains standalone compatibility coverage, not a substitute or correction. See [master-conformance.md](master-conformance.md) for the construct-by-construct findings and commands.
