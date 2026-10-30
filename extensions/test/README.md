# `@test` extension

The `test` bundle adds `@test` as a post-run assertion for `.nflow` acceptance
flows. It accepts the same single-expression, multiline-block, and compact-list
forms as `@assert` and records checks for the existing runner assertion
machinery.

```nflow
const @{ value: "ready" }
@test: result == "ready"
```

The shipped CLI mounts this extension only while `nflow test` executes each
entrypoint. The bundle is deliberately absent from `native.Bundles` and is not
registered with the process-wide `@require` registry. Normal `nflow run`, lint,
and other commands therefore do not recognize `@test`. Existing `@assert`
behavior is unchanged.
