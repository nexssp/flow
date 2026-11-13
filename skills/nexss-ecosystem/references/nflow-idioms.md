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

- Flow accepts both `->` and `|` for pipeline composition. Preserve the repository/user convention;
  for this user's Nexss projects, write `->`. This is a style preference, not a claim that `->` is
  universally more idiomatic.
- Prefer Flow's reserved keyword forms for built-in runtime capabilities whenever available, e.g. `const`, `noop`, `json`, `pick`, `wrap`; use canonical action IDs only when an API requires them or when explaining registry internals.
- For repeated identity values such as message roles, modes, and tool identifiers, use compact bare `@symbols` members. These are workflow-local constants, not registered actions or bundle APIs; do not create a macro for constants. Reserve macros for repeated parameterized source patterns.
- Use projection shorthand deliberately: `{ task, tool, tool_call_id }` copies same-named fields from the current value, while `{ .request.task, .request.tool_call_id }` derives output keys from the final path segments. Do not use a bare nested name such as `request.task` as path shorthand.
- Use object projections to reshape or calculate values instead of adding pass-through or field-copy actions. Use projection-local `let` bindings when they make repeated calculations clearer.
- Use the smallest explicit argument map, `@{ ... }`, for action inputs. It is a data-value block, not a general-purpose expression language; use a projection or a condition for calculations unless the current expression-valued argument form is explicitly supported.
- Before finalizing, remove unnecessary stages, expanded aliases, repeated literals, and repeated flow fragments. Then format, lint, and run the result; concise syntax still has to be valid.

For pure, file-local calculations, use `@func` rather than embedding repeated expressions or adding Go. For example, Flow's current shipping fixture declares a typed `commerce.baseShippingMinor` helper and calls it from another function and projection; see [`extensions/func/nflows/reusable_shipping_fee.nflow`](https://github.com/nexssp/flow/blob/main/extensions/func/nflows/reusable_shipping_fee.nflow). A function body is one pure expression: it does not perform I/O or call actions.

Do not claim that every keyword is available in an arbitrary custom host. The current [Flow language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md) lists the reserved keywords and their canonical mappings.

## Keep model-visible tool contracts separate

- In `@schema`, omit `json` tags when the default JSON key is correct: Flow derives it from the field name (lowercasing the initial letter). Add `json:"..."` only to rename the wire key; preserve `default` and `validate` tags that carry behavior. Tag contents are static metadata, not arbitrary expression positions. Current Flow explicitly supports declared-symbol substitution in schema `default`/`validate` tags when the namespace is declared first; use only that documented feature and recheck the [symbol rules](https://github.com/nexssp/flow/blob/main/docs/symbols.md).
- To expose a pipeline as an AI function, declare both `:tool-schema=SchemaName` and `:tool-description="..."`. Use `ai.tool_specs` to derive provider-ready tool declarations from that `@pipeline` metadata and the referenced `@schema`, then pass its `ai_tool_specs` field to the model call rather than duplicating provider JSON. Confirm the active AI bundle registers this action and supports these modifiers; see the [AI tool metadata guide](https://github.com/nexssp/ai/blob/main/nflows/README.md#function-tools-from-nflow-metadata).
- Keep the model-visible tool-argument schema separate from the pipeline’s internal request/dispatch schema; do not expose internal envelope fields as model arguments. For a true zero-argument tool, use an empty schema such as `@schema ToolArguments {}` rather than adding dummy parameters.
- When normalizing model tool calls, validate the expected call count before indexing. After confirming exactly one call, use a projection-local binding such as `let call = .tool_calls[0];` to read its fields once, construct a normalized flat value, and dispatch on its normalized tool name against an allowlist. This avoids repeating fragile nested paths and prevents unchecked first-call routing.

### Concrete one-call tool pattern

Use the following syntax relationships for a schema-bound LLM tool round trip. Adapt provider settings and tool implementations to the active host, but keep the declarations, metadata, normalization, symbols, and message sequence distinct.

Configure a named model once and reuse it as `llm.mercury3`; callers do not repeat the model:

```nflow
@llm mercury3 {
  provider: "openai-compatible",
  model: "mercury-3",
  base_url: "https://api.inceptionlabs.ai/v1",
  api_key_env: "INCEPTION_API_KEY",
  timeout: "45s"
}
```

Default JSON names need no explicit `json` tag (`task` stays `task`); retain validation/default tags. The sample uses literal tag values, while Flow's limited symbol substitution in schema tags is a separate documented feature.

```nflow
@schema TaskText {
  task string `validate:"required,min=1"`
}

@schema ToolTaskInput {
  TaskText
  tool_call_id string `validate:"required,min=1"`
  tool_name string `validate:"required,min=1"`
  tool_arguments string `validate:"required"`
}

@schema MercuryTask {
  TaskText
  mode string `default:"tool" validate:"required,oneof=ask tool"`
}

@schema ToolArguments {}

@symbols message_roles { system, user, assistant, tool }

@symbols task_modes { ask, tool }

@symbols tool_names { count_words, text_stats }
```

Use the symbols in match arms, result values, and message roles rather than repeating those strings:

```nflow
match(.mode) {
  task_modes.ask
    -> pipeline.ask_mercury,
  task_modes.tool
    -> pipeline.choose_tool -> pipeline.dispatch_tool -> pipeline.finish_tool
}
```

Keep model-visible modifiers separate from the internal input schema. These are the exact modifier lines inside a tool pipeline declaration:

```nflow
:tool-schema=!ToolArguments
:tool-description="Count the words in the full user-provided task using the isolated word-count pipeline."
:in-schema=!ToolTaskInput
```

Build provider tool declarations from the pipeline metadata and pass `prepared.ai_tool_specs` as the model request's `tools` value:

```nflow
@pipeline choose_tool
  noop as caller
  -> ai.tool_specs as prepared
  -> {
      task: caller.task,
      user: caller.task,
      system: "Choose exactly one appropriate tool for the user's request. Call it with an empty argument object. Do not answer until a tool has run.",
      tools: prepared.ai_tool_specs,
      tool_choice: "required",
      max_tokens: 256
    } as request
  -> llm.mercury3 as selection
  -> { task: request.task, tool_calls: selection.tool_calls, model: selection.model }
@end
```

Guard the count before indexing, bind the selected call once, normalize its envelope, and route only allowlisted symbol tool names:

```nflow
@pipeline dispatch_tool
  noop
  -> match {
      len(.tool_calls) == 1
        -> {
            let call = .tool_calls[0];
            .task,
            tool_call_id: call.id,
            tool_name: call.function.name,
            tool_arguments: call.function.arguments
          },
      _
        -> fail @{ message: "Mercury must return exactly one tool call" }
    }
  -> match(.tool_name) {
      tool_names.count_words
        -> pipeline.count_words,
      tool_names.text_stats
        -> pipeline.text_stats,
      _
        -> fail @{ message: "Mercury returned a tool name that is not allowlisted" }
    }
@end
```

A leading-dot nested path derives a key from its last segment; bare fields copy same-named values from the current projection scope. Return the full input envelope with the tool result, then use bare fields in the final result projection:

```nflow
-> {
    .request.task,
    .request.tool_call_id,
    .request.tool_name,
    .request.tool_arguments,
    tool: tool_names.count_words,
    tool_result: execution.stdout
  }

-> {
    mode: task_modes.tool,
    task,
    tool,
    tool_call_id,
    tool_result,
    answer: answer.content,
    model: answer.model
  }
```

For the final model turn, include the original user message, assistant tool-call envelope, and matching tool result, with role values from `message_roles`; reuse the named LLM:

```nflow
@pipeline finish_tool
  noop as tool
  -> {
      messages: [
        { role: message_roles.system, content: "Answer the original request using the tool result. Be concise and do not invent results." },
        { role: message_roles.user, content: tool.task },
        { role: message_roles.assistant, tool_calls: [{ id: tool.tool_call_id, type: "function", function: { name: tool.tool_name, arguments: tool.tool_arguments } }] },
        { role: message_roles.tool, tool_call_id: tool.tool_call_id, name: tool.tool_name, content: tool.tool_result }
      ]
    } as prompt
  -> llm.mercury3 as answer
  -> {
      mode: task_modes.tool,
      task,
      tool,
      tool_call_id,
      tool_result,
      answer: answer.content,
      model: answer.model
    }
@end
```

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
- `@require` adapter selection depends on the resolver and repository layout. In one tested setup,
  the current Flow CLI resolved `@require github.com/nexssp/lang main as lang` from the repository
  root to its `nexssflow` adapter; the [Lang README](https://github.com/nexssp/lang/blob/main/README.md)
  documents `github.com/nexssp/lang/nexssflow` as the released adapter package path. Treat the root
  form as verified behavior for that setup, not a universal rule; check the target resolver and adapter
  docs.
- For compile/runtime boundaries, errors, retries, schemas, approvals, durability, and stream behavior, read [Flow runtime practices](flow-runtime-practices.md) and the linked current docs.
- Follow the current [Flow language walkthrough](https://github.com/nexssp/flow/blob/main/docs/language.md), [implementation-backed language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md), [CLI reference](https://github.com/nexssp/flow/blob/main/docs/cli.md), and [extension guide](https://github.com/nexssp/flow/blob/main/docs/extensions.md) when details change.
