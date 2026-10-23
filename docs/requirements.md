# Managing requirements

`@require` declarations in a `.nflow` file select extension bundles and remote Go modules. Requirement management operates on the direct declarations in the one named file; it does not rewrite included files or create a dependency lock.

## Commands

```sh
nflow require check <file> [--package <pattern>]
nflow require update <file> [--package <pattern>]
nflow require pin <file> [--package <pattern>]
nflow pin <file> [--package <pattern>]
nflow require reset <file>
```

- **`check` is read-only.** It reports each selected remote requirement's current canonical selector and the latest available version. It exits 1 when a source update is available, including when the selector needs canonicalizing even though it resolves to the same version; it exits 0 when no changes are needed and 2 on error. It may use Go's module cache or network, but leaves the source and sidecars untouched.
- **`update`** queries the latest version for each selected direct remote requirement and updates that requirement's selector in the named file.
- **`pin`** canonicalizes each existing selector in the named source. Existing branches, tags, hashes, and `latest` are resolved; a remote requirement with no selector resolves to `latest` only when no local/workspace provider supplies that module.
- **`nflow pin`** is a top-level alias for `nflow require pin`, not a separate pinning implementation.
- **`reset`** restores the named file from its adjacent backup and consumes that backup.

Without `--package`, an operation selects all direct requirements in the named file. `--package` accepts an exact import path or a glob pattern matched against the requirement import path. Matching uses Go `path.Match` semantics, with one deliberate convenience: the pattern `*` alone matches import names across path separators (all direct imports). Other glob patterns retain normal segment behavior, so `github.com/acme/*` matches one path segment, not arbitrarily deep descendants. Quote glob patterns in shells to prevent shell expansion.

Local, workspace, and built-in requirements are skipped by update/pin and are not remote version queries for check. Resolution uses the shared requirement resolver and preserves local-provider precedence. Source edits are token-based: comments, aliases, options, and formatting outside the selector are preserved. A header changed by preprocessing is inspectable but cannot be automatically rewritten; make its import and selector literal first.

## Source backup

A source-changing operation keeps a single exact pre-change source stage at the adjacent path `<file>.bak` (for example, `flows/client.nflow.bak`). It does not rotate or toggle backups. Reset copies that saved source back into `<file>` and removes the backup. Each successful source change replaces that backup with the immediately preceding source stage; it is one-step undo, not a version history. Use Git for longer history. A no-op or resolution failure leaves the backup unchanged. Source and backup are individually atomic writes, not one filesystem transaction: a crash between them may leave the backup equal to the unchanged source. Do not run source-editing commands concurrently on the same file.

`run`, `build`, and `lint` do not require a backup and do not enforce or emit requirement lock files. `.lock` files are ignored/not emitted. `go.sum` is only temporary Go dependency-resolution bookkeeping here, not a project artifact.

## Resolution limits

These commands select or pin **direct source selectors only**. They do not freeze a transitive dependency graph, its checksums, the Go toolchain, or the result of future builds. Updating a direct requirement can change transitive dependencies and can break compatibility; test the flow and its required bundles after changes. A pinned direct version is not a promise that all dependency behavior remains unchanged across environments.
