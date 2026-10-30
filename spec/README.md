# Integrated specifications

`spec/` contains deterministic, end-to-end scenarios that combine multiple Flow features and domain actions. These are acceptance-style examples: they show how a real workflow fits together and assert the result, rather than isolating one extension's syntax.

## Scenarios

1. **[Order intake](01_order_intake/main.nflow)** validates an order, normalizes the customer's email, calculates a safe subtotal, applies a bounded retry policy, enforces a macro invariant, and returns an explicit accepted-order record. The separate **[email normalization check](01_order_intake/normalize.nflow)** sends a lowercase email with surrounding spaces directly to `order.normalize`, bypassing schema validation so its assertion isolates whitespace trimming; the main scenario retains uppercase-to-lowercase coverage.
2. **[Support triage](02_support_triage/happy.nflow)** validates and classifies a duplicate-charge ticket, routes it to the billing escalation queue, and creates a reply draft without sending a customer message. The [unavailable-upstream case](02_support_triage/fallback.nflow) preserves the ticket for manual review and explicitly keeps the reply unsent.
3. **[Payment reconciliation](03_reconciliation/main.nflow)** matches payment and bank references, reports mismatches and bank-only rows as audit exceptions, calculates totals in integer cents, and asserts that the batch variance is zero.

## Structure and conventions

Each scenario is self-contained. When a workflow needs domain-specific Go actions, keep their implementation in that scenario's `bundle/library.go` and require it from the flow:

```nflow
@require ./bundle
```

The local package uses the repository's single root Go module; do not add a nested `go.mod`. Keep the NFlow entrypoint beside its scenario bundle, use additional entrypoints for meaningful paths such as happy, failure, and recovery, and make assertions describe observable outcomes. Use fixed inputs and local deterministic behavior so the checks run offline and do not send messages, change accounts, or mutate external systems.

Use **`extensions/<name>/nflows/*.nflow`** for a single extension's conformance examples, with descriptive filenames and flat companion files. Use **root `spec/`** when the value is the composition: multiple extensions, local domain actions, and a realistic workflow contract. Tutorial-first walkthroughs live in the central [examples repository](https://github.com/nexssp/examples/tree/main/flow).

Run all integrated scenarios directly with:

```sh
nflow test ./spec
```

`nflow test [directory]` runs every `.nflow` entrypoint one directory below the target through the normal Flow execution path, which evaluates existing `@assert` directives. The one-level discovery deliberately includes all entrypoints beside scenario bundles while leaving nested included helpers such as `02_support_triage/common/triage.nflow` to be loaded by their parent workflow. Every matched entrypoint is attempted; any execution or assertion failure makes the command return a non-zero exit status. Add new scenario entrypoints beside their scenario bundle; they are discovered automatically. No `@test` DSL is needed.

`task test:nflow` is the focused Task alias. The existing `task specs` command remains available for compatibility and CI uses it to run the same suite.

`task ci`, also invoked by the GitHub Actions CI workflow, runs `task specs` together with the native extension self-tests and the Go lint/test gates. `task test` runs Go tests, including the extension conformance gate and host-configured embed fixture, but it does not execute the integrated scenarios. The CLI extension self-test gate is also useful on its own:

```sh
task self
```
