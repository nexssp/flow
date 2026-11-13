# NFlow idioms and reuse

Read this before writing or explaining nontrivial `.nflow` syntax. Recheck the current Flow language reference, actual extension fixtures, and active host registry whenever compiler behavior or bundle availability matters.

## Use real syntax evidence

Treat `extensions/**/nflows/*.nflow` as the primary fixture source for syntax and extension behavior, and `spec/**` as integrated acceptance scenarios for workflows that combine bundles or local actions. Check the current files: the top-level Flow `examples/README.md` may only redirect to the separate examples repository, not contain runnable Flow fixtures. Do not describe a fragment as runnable unless its paths, declarations, required bundles, and host match the example.

The language reference documents the current parser and compiler behavior; the active flow registry determines which capabilities actually resolve. A valid keyword or operator does not make every canonical action available in an arbitrary custom host.

## Treat the language as domainless composition

A flow composes registered actions and expressions; it does not define a fixed business vocabulary. The action namespace identifies a capability's owner, but a name is valid only if the active host has registered it. Do not infer DSL syntax from a Go package name, a README heading, or a directory under `extensions/`.

Use this discovery order before inventing a missing feature:

1. Inspect the actual flow's registry with current `nflow list`, `nflow show`, or `nflow catalog` CLI forms; use `nflow explain` to inspect effective modifiers when relevant.
2. Check its `@require` declarations and host wiring; the default CLI and a custom host may expose different bundles.
3. Search current extension NFlows, integrated specs, bundle source, and tests for the exact action name, argument shape, directive, modifier, or operator.
4. If the capability is absent, first check whether existing actions can be composed. Add the smallest bundle capability only when composition/reuse is inadequate.

## Apply the concision rule

For new flow syntax, actively choose the shortest supported expression that preserves meaning and readability. Do not mechanically copy expanded or compatibility forms when a documented shorthand exists.

- Prefer `|` over `->` for the same pipeline connection; `|` is documented as a pipeline alias.
- Prefer reserved keyword actions such as `const`, `noop`, `json`, `pick`, and `wrap` over their canonical action names when both express the same operation. Use canonical names when an API explicitly requires them, when inspecting registry output, or when the short form would obscure intent.
- For an ordinary constant action, write `const @{ value: "..." }`, not `runtime.const @{ value: "..." }`. Keep the qualified form for examples specifically about canonical registry names or when that name is required.
- Use object projections to reshape or calculate values instead of adding pass-through or field-copy actions. Use projection-local `let` bindings when they make repeated calculations clearer.
- Use the smallest explicit argument map, `@{ ... }`, for action inputs. It is a data-value block, not a general-purpose expression language; use a projection or a condition for calculations unless the current expression-valued argument form is explicitly supported.
- Before finalizing, remove unnecessary stages, expanded aliases, repeated literals, and repeated flow fragments. Then format, lint, and run the result; concise syntax still has to be valid.

For pure, file-local calculations, use `@func` rather than embedding repeated expressions or adding Go. For example, Flow's current shipping fixture declares a typed `commerce.baseShippingMinor` helper and calls it from another function and projection; see [`extensions/func/nflows/reusable_shipping_fee.nflow`](https://github.com/nexssp/flow/blob/main/extensions/func/nflows/reusable_shipping_fee.nflow). A function body is one pure expression: it does not perform I/O or call actions.

Do not claim that every keyword is available in an arbitrary custom host. The current [Flow language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md) lists the reserved keywords and their canonical mappings.

## Reuse the right construct

Use the smallest construct that matches the reuse boundary:

- **`@func`**: a pure, typed expression helper for repeated calculation. It cannot perform I/O or invoke actions. It is file-local; an included module exposes it under that module's namespace.
- **`@macro`**: a short, parameterized source pattern that should expand at the call site. Use it for genuine repeated syntax; do not use it to hide a complex workflow or create an unreadable mini-language. Macros substitute source expressions and re-parse the expansion, so verify the current parameter syntax, kinds, arity, and limits.
- **`@pipeline`**: a named flow-level composition that should be invoked as a pipeline capability. Use it when readers benefit from a descriptive reusable workflow name.
- **`@profile` / `@scope`**: reusable or locally scoped policy modifiers. Check the current reference for inheritance and where profiles are accepted; a pipeline profile is not a generic action modifier.
- **`@include`**: import another `.nflow` source module with its own namespace and exported declarations. It does not textually merge constants or profiles into the parent and does not load a Go bundle.
- **`@require`**: load a Go extension bundle/module so its registered actions or compiler extensions can be used. It is distinct from `@include`. Use current resolver rules; omit version pins in ordinary moving-branch work, as required by the ecosystem's no-pinned-versions policy.

These source forms are demonstrated in the current repository:

```nflow
@require ./bundle
@include ./child.nflow AS billing
```

The `@require` line is the local bundle declaration used by integrated scenarios such as [`spec/01_order_intake/main.nflow`](https://github.com/nexssp/flow/blob/main/spec/01_order_intake/main.nflow); it is valid only when that scenario's `./bundle` is a Flow bundle. The include form is used in the extension fixture [`extensions/include/nflows/pipeline_include.nflow`](https://github.com/nexssp/flow/blob/main/extensions/include/nflows/pipeline_include.nflow). These paths are fixture-specific, not universal import paths.

When a required bundle explicitly exports embedded `.nflow` modules, load them separately with the bundle alias and `@include alias:path` form. Do not assume every `.nflow` file in a bundle is exported. Do not use angle-bracket pseudo-paths as if they were pasteable grammar.

## Do not conflate the mechanisms

| Need | Use | Does not mean |
|---|---|---|
| Short equivalent flow syntax | documented keyword or operator sugar | a new registered action in every host |
| Pure calculation reused in expressions | `@func` | an action, schema, or workflow boundary |
| Repeat a parameterized source fragment | `@macro` | a separate module or runtime function |
| Name a composed workflow | `@pipeline` | an external capability provider |
| Reuse another `.nflow` file | `@include` | loading every bundle in that repository |
| Make bundle capabilities available | `@require` | importing arbitrary Flow source declarations |
| Add low-level action behavior | existing or new Go bundle | move orchestration out of NFlow |

A CLI is an adapter over a workflow, not a reason to move workflow policy out of NFlow. For a single `.nflow` entrypoint that exposes TCLI commands and can be built into an executable, keep the command binding and listener in that flow and build the same source with `nflow build`; do not assume TCLI requires a separate Go wrapper. See [NFlow build and TCLI entrypoint](nflow-build-and-tcli.md) for the verified pattern.

## Evidence and correctness notes

- A macro is compile-time expansion, not runtime isolation. Do not mistake expansion hygiene for security.
- A bundle's internal `compiler.Bundle` fields are host integration points, not automatically user-facing NFlow syntax. Verify user syntax in extension NFlows or integrated specs.
- `@include` scopes declarations under its namespace. Pipelines, schemas, symbols, and macros can be imported; constants and profiles remain local to their source module.
- A static lint pass checks the preprocessed source and names against its registry context, but it cannot prove all dynamic payloads, runtime values, or external side effects.
- For compile/runtime boundaries, errors, retries, schemas, approvals, durability, and stream behavior, read [Flow runtime practices](flow-runtime-practices.md) and the linked current docs.
- Follow the current [Flow language walkthrough](https://github.com/nexssp/flow/blob/main/docs/language.md), [implementation-backed language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md), [CLI reference](https://github.com/nexssp/flow/blob/main/docs/cli.md), and [extension guide](https://github.com/nexssp/flow/blob/main/docs/extensions.md) when details change.
