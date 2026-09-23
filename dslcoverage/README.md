# Flow DSL Coverage Suite

Every command, directive, and modifier the flow DSL supports has one
`.nflow` file here. Running `go test ./flow/testdata/coverage/...`
executes every file and reports which ones pass.

## File rules

| Suffix | Meaning |
|---|---|
| `*.nflow` | Test. Executed by the harness. |
| `*_helper.nflow` | Fixture for `@include`. Not executed. |
| `*_service.nflow` | `@on` file. Preprocessed only, not executed. |
| `skip_*.nflow` | Ignored. Used for parked failures. |

## File requirements

- Must declare at least one `@assert:`.
- Must not depend on the network, filesystem, or env vars, except
  where the test *is* the dependency (env, filesystem).
- Must be deterministic. Random UUIDs are fine; random outcomes are not.

## Adding a new file

1. Pick the category folder.
2. Name the file after the feature.
3. Write the smallest possible flow that uses it.
4. Assert the outcome with `@assert:`.
5. Run `go test ./flow/testdata/coverage/...` and confirm it passes.

## What failure means

If a file fails, one of two things happened:

- The feature is broken. Fix the feature, then the file passes.
- The feature is not implemented yet. Rename the file to `skip_*.nflow`
  and open an issue.

Either way the suite tells you the truth about the current state.

## Inventory

See the inventory in the coverage PR description. It lists every
`.nflow` file the suite expects. If a file is missing, add it; if a
file exists without a matching inventory entry, fix the inventory.
