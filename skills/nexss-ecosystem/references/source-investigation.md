# Source investigation with srcpack

Use srcpack to build focused, provenance-preserving source context; it reads source and does not edit repositories. It is a separate CLI, not installed by this skill. Check `command -v srcpack` first. If absent and installation is permitted, inspect srcpack’s current `go.mod` and use the latest stable Go toolchain it supports with `go install github.com/nexssp/srcpack/cmd/srcpack@latest`; ensure `GOBIN` or `GOPATH/bin` is on `PATH`, then verify with `srcpack --help` and `srcpack list`. Otherwise use focused repository-native tools and report the limitation. The patched binary installed in this environment reports its Go module version rather than `dev`; other or older builds may differ. Verify with `srcpack --version`, and inspect `go version -m "$(command -v srcpack)"` when module metadata matters.

The installed CLI is the contract: inspect `srcpack --help`, `srcpack list`, and `srcpack cookbook` before relying on flags or config syntax. Moving `main` documentation may describe features not present in an installed release.

## Choose a small source slice

1. Map only the likely subsystem before reading bodies:

   ```sh
   srcpack tree "compiler" --tree-files --depth 2 -o -
   ```

2. Preview selector and output settings before a broad pack:

   ```sh
   srcpack pack "compiler#dense" --plan
   ```

   Plan mode does not enumerate candidate files or estimate final context size. Use the actual pack's reported file and token counts to decide whether to narrow it further.

3. Select the smallest owning directory or file, then the detail needed. Do not pack `.` or an entire repository by default. Use a file line slice when the behavior is localized:

   ```sh
   srcpack "compiler/compile.go:80-140#source" -o -
   srcpack "compiler#api" -o -
   srcpack "extensions/macros#with-tests" -o -
   ```

   Quote selectors so the shell preserves `#`, `~`, `@`, and line-range syntax.

4. Use local Git filters for changed work only, from the target worktree:

   ```sh
   srcpack ".#impl" --modified --diff-only -o -
   srcpack ".#with-tests" --since origin/main -o review.md
   ```

   `--modified`, `--staged`, `--since`, and `--changed` describe the local workspace; do not combine them with remote GitHub selectors. Add `--diff-only` only when the unified diff is relevant.

## Select the profile per task

- `arch`: declaration/signature map without bodies or comments; use for structure.
- `api`: public signatures with comments but no bodies; use for contracts and interface documentation.
- `impl`: implementation bodies with comments stripped; use for focused logic review.
- `source`: selected source with bodies and comments, without tests.
- `with-tests`: selected source, comments, and tests; use for behavior questions, scoped to the owning package.
- `dense`: compact signature-oriented context; useful for broad Go structure when signatures are enough.
- `compact`: trimmed/limited source, but not a guarantee of the smallest total context for a large directory. Prefer narrowing paths first and compare the actual token statistics; in a multi-file compiler check, `compact` produced substantially more output than `dense`.

Profiles are independent per selector, so mix them when useful. Bodies and tests cost tokens: start with `arch`, `api`, or `dense`, then add `impl`, `source`, or `with-tests` only for the specific owner. A profile never makes an overly broad selection automatically safe or small.

## Repeat only stable selections

No config file is needed for one-off use. `nexss.srcpack.yml` is workspace-specific and can hold:

- **profiles**: reusable rendering/transformation settings; built-in profiles usually suffice.
- **targets**: named local directory/file groups, invoked according to the installed CLI's help.
- **views**: flat saved selector sets; use them for repeated source combinations, then combine multiple view names as separate arguments on a run.

Use configured domain views for recurring Nexss context. Create another view only after a narrow selector set proves useful more than once. A view selects sources; a profile chooses how each source is rendered. Saving stores selectors; run the view separately to create output:

```sh
srcpack "compiler#dense" --save-view flow-compiler-map
srcpack flow-compiler-map -o flow-compiler-map.md
srcpack view list
srcpack view show flow-compiler-map
srcpack config validate
```

The Flow checkout's `nexss.srcpack.yml` defines the optional `flow-compiler-map`, a small `nexss-core`, and flat cross-repository domain views. See [Nexss srcpack view catalogue](srcpack-views.md) for their scope and safe composition. The CLI expands multiple top-level view names in one invocation, but does not recursively expand a view name stored inside another view. Use the compiler view only when compiler structure is relevant; it is not default context for every Nexss task.

## REST API source and live endpoints

For REST API code, select the confirmed route/router, handler, schema or OpenAPI document, client/server adapter, and relevant tests. Use `api` for Go contracts, `impl` or `source` for implementation, `with-tests` for behavior, and `source` for textual specs. For example, replace these placeholders with paths verified in the actual repository:

```sh
srcpack "path/to/http-handlers#with-tests" "path/to/openapi.yaml#source" -o rest-api-review.md
```

srcpack packages source; it does not call a live REST endpoint or fetch arbitrary API responses. Its remote-source form is srcpack-specific selector syntax, not a GitHub URL or Go import path:

```text
github:OWNER/REPO[//PATH][?ref=REF][#PROFILE]
```

For example, `github:nexssp/transportai//ta2a#api` selects the public repository's `ta2a` subdirectory and renders its API profile. `github:` is required, `//` introduces the repository-relative path, and `#api` selects the profile. A value such as `github.com/nexssp/transportai` or `https://github.com/nexssp/transportai` is treated as a local path, not as a remote selector. Remote reads require Git and public repositories; private credentials are not forwarded. Use an appropriate HTTP/API client for live endpoint checks, and treat authentication, writes, and other external effects separately. The srcpack repository's `tests/fixtures/nflows/serve_rest.nflow` is a test fixture, not a general REST-source connector.

Remote selectors omit `?ref=` to follow the current default revision; use a ref only for an explicit historical or reproducible-source question. Remote sources cannot be combined with local-change flags. Before sharing a generated pack, review its file list and content.

## Provenance and configuration safety

Rendered file blocks include a machine-readable `srcpack-source` location comment with original path and line range; Go signature profiles may also include symbol spans. Cite original source paths/lines, not line positions in the rendered package, and inspect the source itself when a claim matters.

`--include-config` is opt-in. Use it only when build, tooling, or CI configuration affects the question. Secret detection/masking is best-effort; credential files and `.env` variants are excluded by default, but always review generated context before sharing it. Selector `@...` is per-file metadata and may not render in Markdown; use inline `--prompt "..."` when the instruction must appear in the package.

## Official references

- [srcpack README and selector/profile guide](https://github.com/nexssp/srcpack/blob/main/README.md)
- [srcpack bundled cookbook](https://github.com/nexssp/srcpack/blob/main/bundles/system/cookbook.md)
- [srcpack selector cheat sheet](https://github.com/nexssp/srcpack/blob/main/docs/cheatsheet.md)
- [srcpack GitHub source implementation](https://github.com/nexssp/srcpack/blob/main/bundles/github_source/remote.go)
- [Flow CLI reference](https://github.com/nexssp/flow/blob/main/docs/cli.md)
- [Flow language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md)
- [Flow extension guide](https://github.com/nexssp/flow/blob/main/docs/extensions.md)
- [Kernel README](https://github.com/nexssp/kernel/blob/main/README.md)
