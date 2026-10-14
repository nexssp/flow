# Approval pause and resume

Flow's native `approval` extension provides declarative approval gates. A gate uses Kernel's `dag.Suspend` sentinel; the runner turns that into a persisted pause when its journal implements `runner.SuspensionStore` (the built-in `FileJournal` does).

```nflow
@approval finance_review {
  role: finance reviewer
  reason: confirm the transfer
  expires: 1h
}

payments.prepare -> approval.finance_review -> payments.commit
```

The materializer registers `approval.finance_review` for that source. The gate passes its input through unchanged only when the Kernel context carries an approval token covering `finance_review`; otherwise it suspends with the declared role and reason. The default expiry is 30 minutes. `expires` must be a positive Go duration.

## CLI flow

Start the workflow with the journal enabled from the beginning:

```sh
nflow run transfer.nflow '{"amount": 125}' --journal-dir .flow-state
```

On suspension, the CLI reports the gate, role, reason, deadline, and generated execution ID, then prints a resume command. Resume using the same source, exact input payload, journal directory, and execution ID, and add the matching gate token:

```sh
nflow run transfer.nflow '{"amount": 125}' \
  --journal-dir .flow-state \
  --execution-id flow-<id-from-first-run> \
  --approval=finance_review
```

The `--approval` value may be a comma-separated list of case-sensitive gate names, or `all`. Both `--approval VALUE` and `--approval=VALUE` are accepted. A suspended run exits with status 3. A subsequent run without a matching token returns the existing pause without executing workflow actions.

The runner records the pending gate and request metadata separately from durable step results. It also binds the pending request to the exact source name, source text, and JSON-encoded input. A mismatch or expired request is rejected; use a new execution ID to start a changed or expired workflow. On resume, Flow reruns the pipeline from its beginning and Kernel's durable middleware replays previously completed source-positioned capability results, then the gate is re-evaluated and later steps continue. Successful completion clears the pending approval record.

For a persisted pause, use `FileJournal` or a custom `action.Journal` that also implements `runner.SuspensionStore`. Without a suspension-capable journal, Flow can report that a gate suspended, but cannot safely persist or resume the approval request. Enable the journal on the original run, before any side effects that may need replay protection. Keep the source name, source callsites, and input stable; use a new execution ID for revised input or workflow.

## Security boundary

The token check is a gate-name match, not an identity provider or signed approval credential. `role` and `reason` are descriptive metadata; Flow does not authenticate the person who supplies `--approval`, verify their role, or issue/expire credentials. An embedding application should authenticate the approver and set `xctx.WithApprovalToken` only after its own authorization checks. Do not expose a raw `--approval` flag to untrusted callers as if it were authenticated human approval.

A `FileJournal` is a local persistence mechanism and not an execution-wide lock service. Do not run the same execution ID concurrently, within one process or across processes, when duplicate side effects would be unsafe. Durable results are retained until the journal directory is removed or archived.
