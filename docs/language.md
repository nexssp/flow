# `.nflow` language

A `.nflow` file contains Flow directives and a pipeline expression. An atom names an action available to the runner; operators compose actions, and projection expressions can reshape values between them. The examples linked here are current runnable fixtures.

## Pipeline syntax

| Form | Meaning |
|---|---|
| `A -> B` | Pass the left result to the next action. |
| `A | B` | Alias for `->`. |
| `A || B` | Try the right expression if the left expression fails. |
| `(A & B)` | Run the grouped branches in parallel. |
| `{ key: expression }` | Project fields and expressions into a new object. |
| `action @{ key: value }` | Pass named values as the action's arguments. |
| `action:timeout=1s` | Apply a modifier to that action. |

The binary operators and their current descriptions are also available from `nflow list`. The built-in feature suite tests parallel composition with this expression:

```nflow
( const @{ value: "left" } & const @{ value: "right" } )
```

Parallel results use each branch action's name as its gather key. A name used by only one branch keeps its existing key; repeated names get `name#1`, `name#2`, and so on in source order (for example, `echo#1` and `echo#2`). If a generated label is already in use, trailing `#` characters are added until it is unique. The checked [parallel actions example](../examples/00_flow_basics/07_parallel_actions.nflow) demonstrates this mapping with local actions and asserts the resulting map. For a verified end-to-end projection, see [data transformation](../examples/00_flow_basics/04_data_transformation.nflow). For a working timeout and fallback flow, see [sleep and timeout](../examples/00_flow_basics/02_sleep_and_timeout.nflow).

## Directives

Directives are handled before the pipeline is compiled. Common directives in the current compiler include:

| Directive | Use |
|---|---|
| `@description "..."` | Attach a human-readable description. |
| `@assert: expression` | Check the final result after the flow runs. |
| `@pipeline name` … `@end` | Declare a named pipeline. |
| `@include "path.nflow"` | Include another local Flow file. |
| `@require ...` | Request an extension or library bundle for the flow. |

For example, current example files use `@assert: result.summary == "Ada (ID: 101) is active"` to check their result. Directive and modifier availability can depend on the bundles loaded by a runner; use `nflow list` to inspect the active catalog rather than relying on a fixed feature list.

## Next steps

See the [CLI guide](cli.md) for run, lint, build, and catalog commands, or the [extension guide](../extensions/README.md) for how bundles add compiler features.
