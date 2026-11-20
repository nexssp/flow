# Durable runner execution

Flow uses Kernel's durable middleware to persist both the completed top-level flow result and each source-positioned capability call when `runner.Config.Journal` is non-nil. The runner also generates an execution ID for every `Execute`/`ExecutePrepared` call and returns it as `Execution.ExecutionID`.

```go
journal, err := runner.NewFileJournal(".flow-state")
if err != nil {
    return err
}
defer journal.Close()

cfg.Journal = journal
cfg.IdempotencyStore = journal // optional; used only by atoms marked :idempotent

ctx := xctx.WithExecutionID(context.Background(), "invoice-2026-0042")
execution, err := runner.Execute(ctx, cfg, source, "invoice.nflow", payload)
```

If no ID is supplied, Flow creates a random `flow-<hex>` ID. Save the returned ID and supply the same ID with `xctx.WithExecutionID` (or `runner.Opts.ExecutionID` for `RunSource`) when resuming a run. A new ID intentionally starts a distinct durable execution.

The CLI and generated standalone applications can configure the same file journal directly:

```sh
nflow run invoice.nflow --journal-dir .flow-state
# Copy the `Execution ID: ...` value printed to stderr:
nflow run invoice.nflow --journal-dir .flow-state --execution-id flow-0123456789abcdef0123456789abcdef
```

The separated and equals forms are both accepted (`--journal-dir DIR` or `--journal-dir=DIR`, and likewise for `--execution-id`). An execution ID requires a journal directory on the CLI and app. A journal directory without an explicit ID starts a fresh execution and prints the generated ID so it can be resumed later. Standalone binaries accept the same flags. Keep the source name and callsite layout stable when resuming; use a new ID if the workflow or intended input changes.

## Step identities and replay

A durable capability-step identity is built from the source filename, line, column, and capability name. A per-run occurrence number is appended so a capability inside a loop gets a distinct record on each iteration. The occurrence counters reset for each runner execution and are shared through its context.

The completed entrypoint result is stored as a separate durable step keyed by the source name and entrypoint, so even a flow with no capability calls can replay its prior result. An interrupted run still resumes from completed capability steps; the entrypoint result is recorded only after the whole flow succeeds.

Keep the source name and callsite layout stable when resuming. Reusing an execution ID after changing a workflow or its input can replay an old step or completed flow result, so use a new ID for a revised run. Durable results are retained indefinitely by `FileJournal`; remove or archive the journal directory according to your retention policy.

`FileJournal` atomically writes step records under a confined directory, with hashed filenames. It also implements Kernel's idempotency coordinator/store API, so it can be assigned to `Config.IdempotencyStore`. That store is consulted only for atoms carrying `:idempotent`, and Flow scopes keys to the stable action callsite and occurrence to avoid collisions between different atoms.

## Operational limits

- FileJournal persists completed results and coordinates idempotency claims among goroutines using the same in-process instance. It is **not an execution-wide lock or a distributed lock service**: do not run the same execution ID concurrently, either within one process or across processes, if duplicate side effects would be unsafe. The separate `:idempotent` claim mechanism does not make an entire workflow exclusive.
- Kernel durable middleware serializes erased `any` results as JSON. Replayed data uses JSON-native shapes (for example, numbers may become `float64`); keep durable results JSON-compatible and account for that normalization.
- `:idempotent` without `Config.IdempotencyStore` continues to use Kernel's default in-memory store. With the FileJournal store, cached idempotency responses use Kernel's default TTL unless configured elsewhere.
- FileJournal errors on durable-step reads/writes are returned through Kernel; custom-store `Get` has a legacy no-error contract, so unreadable records fail closed during response decoding. Normal idempotency completion uses the coordinator methods and surfaces persistence failures.
