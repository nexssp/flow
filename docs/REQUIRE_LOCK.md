# Requirement locks

Nexss Flow can freeze the dependency resolution selected by Go for a `.nflow` file into a sidecar lockfile. This stabilizes dependency selection without changing the flow source or its `@require` declarations.

## Freeze a flow

```sh
nflow require freeze flows/client.nflow
```

This writes `flows/client.nflow.lock`. Freeze is an explicit resolution operation: Go selectors in `@require` declarations are resolved using Go's module/package resolution, and the lock records the selected requirements and the complete selected Go module graph, including module checksums. Put the selector after the import path, separated by whitespace. For example:

```nflow
@require example.com/acme v1.2.3
@require example.com/acme release-2026
@require example.com/acme main
@require example.com/acme 0123456789abcdef0123456789abcdef01234567
@require example.com/acme latest
```

These illustrate a version, tag, branch, commit, and the explicit `latest` query. Selectors are passed to Go's module query resolution; use selectors meaningful for the upstream module. If an external requirement is written with no selector (`@require example.com/acme`), only the explicit freeze operation treats it as `latest` when no local/workspace provider supplies it. This does not change existing local/workspace provider precedence. The resulting lock records what was selected, rather than rewriting a moving selector into the `.nflow` file.

`nflow require pin <file.nflow>` and the historical top-level `nflow pin <file.nflow>` are compatibility aliases for exactly the same freeze operation. They do not edit the source. Namespace aliases and other source-level `@require` options remain where they were declared, and continue to be part of the dependency inputs checked against the lock.

Run `nflow help require` for the current command help.

Commit the `.nflow.lock` sidecar to git alongside the flow. Normal `nflow run` and `nflow build` automatically enforce a present lock, checking that it matches the source requirements, toolchain, selected module graph and local dependency fingerprints. Other CLI operations that load the flow's requirements also validate a present lock. The CLI's own Go runtime compatibility is bound separately from the Go toolchain used for dependency-resolution subprocesses; the lock records the effective Go version of that subprocess toolchain. A generated runner checks its runtime against the recorded subprocess toolchain, not against the parent CLI's runtime. If a selector or dependency graph should move, run `nflow require freeze <file.nflow>` again and commit the deliberately updated lock. There is no flag to opt out of or select a separate lock system.

## Local modules and portability

Local and workspace modules, including loose local bundles without a `go.mod`, are fingerprinted, not copied into or vendored in the lock. Implementation-only edits to a loose bundle invalidate the lock even when its imports do not change. Locks created before loose-bundle fingerprints were recorded must be explicitly refrozen when they contain loose bundles. Their source remains at its local/workspace location, and machine-specific paths or workspace replacements can bind the lock to a particular checkout or machine. Keep the required local module available and unchanged, or explicitly refreeze after changes.

The lock binds to the effective Go toolchain version used by the Go subprocesses when it was created; the CLI runtime has its own compatibility check. Module checksums and their verification continue to rely on Go's checksum and trust configuration (including the checksum database where applicable). A requirement lock is not an offline cache, does not replace Go module checksum trust, is not vendoring, and is not a signed manifest. It also does not promise bit-identical builds across operating systems, architectures, or different environments.

## Linting multiple files

Linting an individual locked workflow validates its lock. When linting multiple files requires an external harness, locked workflows must be linted individually; the multi-file external-harness path reports this limitation rather than silently bypassing per-file lock enforcement.
