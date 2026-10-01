# Practical secure-pipeline examples

These flows run from the repository root with the built-in `nflow` CLI. They use fixed local values, the built-in failure action, or the checked-in fixture only; they make no network calls, read no credentials, and perform no filesystem writes.

- [`01_validate_intake.nflow`](01_validate_intake.nflow) places a runtime `assert(...)` before a projection, so missing required values stop later stages. The example checks presence only; it does not validate identity, authorization, email syntax, or every possible type. The predicate is application logic, not a universal policy engine.
- [`02_conditional_hold_gate.nflow`](02_conditional_hold_gate.nflow) chooses a pass-through or a `held` marker from a boolean in the input. This demonstrates control-flow routing only: the flag is local input, not human approval, and neither branch performs a protected external action.
- [`03_bounded_retry_fallback.nflow`](03_bounded_retry_fallback.nflow) applies `:retry=2` to a deterministic local failure, then uses `||` to return a fixed fallback. This proves the error path can recover; it does not prove a transient service will recover. Retries can repeat work, so do not add them blindly to non-idempotent actions. The runtime's retry/backoff policy is not a strict cost or time budget.
- [`04_read_scoped_fixture.nflow`](04_read_scoped_fixture.nflow) walks and reads a single checked-in fixture directory and asserts the expected file. It does not write files. The configured root is a flow argument, not an operating-system sandbox; changing it can expand which files are read.

`nflow lint` checks source preprocessing, syntax, and names against the active registry; it does not execute a flow, validate runtime values, or prove a security property. These flows do not rely on `@schema`: in the current CLI, a declaration alone is not automatically applied to arbitrary untyped actions. Strict request validation is separate and applies only to actions with a typed request contract carrying validation tags; none of these sample pipelines uses one. None of these examples implements approval/HIL, authorization, strict cost enforcement, or process isolation.

Run one flow from the repository root:

```sh
nflow lint examples/01_secure_pipeline/01_validate_intake.nflow
nflow run examples/01_secure_pipeline/01_validate_intake.nflow
```

Run the complete curated set, with coverage checked before linting and execution:

```sh
task examples
```
