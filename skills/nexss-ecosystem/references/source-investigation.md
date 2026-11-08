# Source investigation with srcpack

Use srcpack as the primary tool for building focused source context. It selects and summarizes code for reading; it does not modify repositories. Start with structure, then narrow to implementation and tests. All reusable remote selectors below omit `?ref=`, so srcpack follows each repository's current default revision. Check the current installed CLI and its bundled [cookbook](https://github.com/nexssp/srcpack/blob/main/bundles/system/cookbook.md) because selector and flag details can change.

## Contents

- [Map the ecosystem](#map-the-ecosystem)
- [Select focused source and useful evidence](#select-focused-source-and-useful-evidence)
- [Preserve provenance and handle configuration carefully](#preserve-provenance-and-handle-configuration-carefully)
- [Investigate local changes and preview selection](#investigate-local-changes-and-preview-selection)
- [Follow a feature into source](#follow-a-feature-into-source)
- [Repeat a useful selection](#repeat-a-useful-selection)
- [Trace undocumented needs without guessing](#trace-undocumented-needs-without-guessing)
- [Freshness and source boundaries](#freshness-and-source-boundaries)
- [Official references](#official-references)

## Map the ecosystem

From a working directory with srcpack available, produce one compact architecture context:

```text
srcpack "github:nexssp/kernel#arch" "github:nexssp/flow#arch" "github:nexssp/srcpack#arch" -o nexss-ecosystem-map.md
```

For the current repository, include its local structure too:

```text
srcpack ".#arch" "github:nexssp/flow#arch" "github:nexssp/kernel#arch" -o nexss-project-map.md
```

Use these as initial maps, not inventories of every implementation. Confirm repository and subdirectory names against the current default branch before narrowing.

## Select focused source and useful evidence

Combine selectors to include related packages at appropriate detail levels; use exclusions to keep tests, generated fixtures, or vendor paths out when they are not relevant:

```text
srcpack "cmd,internal#arch" "internal/auth#api" "internal/store#with-tests" -o focused-context.md
srcpack "internal/auth~*_test.go#impl" -o auth-implementation.md
```

For a local file, select an inclusive, 1-based line range when only one behavior is relevant:

```text
srcpack "internal/auth/service.go:80-140#source" -o -
srcpack "internal/auth/service.go:80-#source" -o -
```

Profiles include `arch` for a declaration map, `api` for commented public interfaces, `impl` for implementation bodies, `source` for selected source plus comments, and `with-tests` for source and tests. Use `srcpack list` and `srcpack info <profile>` for the installed profile descriptions. Use `srcpack types` when a file-type alias is unclear.

## Preserve provenance and handle configuration carefully

Rendered file blocks include a machine-readable `srcpack-source` location comment. Its `path`, `start_line`, and `end_line` retain original-file coordinates; Go signature profiles may also provide declaration names/kinds/spans under `symbols`. Use those original locations when citing generated context rather than counting lines in the rendered bundle. The metadata preserves provenance but is not itself a correctness check: inspect the source when a claim matters.

`--include-config` is opt-in. It includes eligible build/tool configuration as raw text; detected secret-like values are masked on a best-effort basis, while `.env` variants and common credential/private-key files remain excluded. This is not a guarantee that configuration is safe to share. Review the generated output before sending or publishing it.

Selector `@...` text is per-file prompt metadata and is not printed by the current Markdown renderer. Use inline `--prompt "..."` when a shared instruction should appear in the rendered package; a path passed to `--prompt` is literal text, not a prompt-file loader. Recheck the current cookbook when relying on prompt-rendering behavior.

## Investigate local changes and preview selection

Run local-change selectors from a Git worktree. Choose the set that matches the review question:

```text
srcpack ".#impl" --modified --diff-only -o modified-review.md
srcpack ".#source" --staged -o staged-context.md
srcpack ".#with-tests" --since origin/main -o branch-context.md
srcpack ".#arch" --changed -o changed-since-last-run.md
```

`--modified`, `--staged`, `--since`, and `--changed` select local workspace changes; do not combine them with remote GitHub selectors. Add `--diff-only` when the Git diff should be included. Inspect the current CLI help for exact semantics of `--changed` and saved incremental state.

Before generating a large selection or writing an output, use `srcpack pack <selector> --plan` to preview the resolved selection, and use `--check` to see whether a generated output would change. Use `srcpack tree <path> --tree-files --depth N` when path layout or selector coverage is unclear. These are selection/preview tools, not substitutes for reading the resulting source.

## Follow a feature into source

After the map points to the likely owner, select only the relevant paths. For example, when tracing a compiler feature:

```text
srcpack "github:nexssp/flow//compiler#api" "github:nexssp/flow//extensions/macros#source" -o flow-compiler-macros.md
```

For a behavior question, include the owning package's tests or executable flow fixtures:

```text
srcpack "github:nexssp/flow//extensions/macros#with-tests" -o flow-macro-evidence.md
```

For Flow language syntax, verify the compiler/docs against actual `extensions/**/nflows` fixtures. For workflows combining several capabilities, inspect `spec/**`. The Flow top-level `examples` directory may only be a README redirect to the central examples repository; inspect its contents rather than assuming it holds fixtures.

For local edits, inspect changed context instead of rereading an entire checkout:

```text
srcpack ".#arch" --modified -o changed-architecture.md
srcpack ".#source" --modified --diff-only -o changed-source-and-diff.md
```

Quote selectors so shells do not reinterpret `#`, `@`, `~`, or other selector characters. Use `srcpack --help`, `srcpack pack --help`, `srcpack list`, `srcpack info <profile>`, and `srcpack cookbook` for current CLI usage and examples.

## Repeat a useful selection

When a selection is reused regularly, save it as a named view; saving or deleting views and editing targets/prompts changes the workspace's srcpack configuration. Inspect current help before using configuration-changing commands. For setup or troubleshooting, the current cookbook documents `srcpack init`, `srcpack doctor`, and `srcpack config validate`; do not run initialization or mutate configuration unless it is needed for the task.

## Trace undocumented needs without guessing

1. Translate the user's wording into the input/output behavior they need; keep the domain name separate from the capability.
2. Search the current Flow registry, source tree, runnable `.nflow` fixtures, adapter/bundle source, and tests. Inspect Kernel separately when the question is about execution semantics rather than the DSL surface.
3. Record each candidate as **verified**, **not found in inspected sources**, or **unverified**. A missing hit is not proof that no other host or private bundle can provide the capability.
4. For verified syntax, cite a current extension fixture or integrated spec. For bundle behavior, inspect both the `@require`/registration path and the action implementation. For Kernel behavior, cite the owning package and its tests.
5. If no existing capability fits, identify whether NFlow composition suffices. If not, place only the missing action-level behavior in `bundles/` and keep workflow/orchestration in NFlow.
6. Validate the proposed flow against the same active registry it will use. Prefer `nflow fmt --check` and `nflow lint` for static checks, then run the specific example/test or a safe representative payload for runtime evidence.

A useful evidence ledger is short: **claim → current file/test and original source line → what was actually verified → any host/config prerequisite**. Keep unknowns explicit and link sources at the claim they support.

## Freshness and source boundaries

- Remote GitHub selectors without a ref follow the current default revision. Local checkouts may be ahead of or behind that default; inspect their branch/status before drawing conclusions.
- Remote source requires public repositories and Git; private-repository credentials are not forwarded. If remote access is unavailable, use the user's existing local checkout or report the blocker.
- Do not put release tags or commit hashes into reusable skill examples, repository inventories, or ordinary source selectors. Use a historical ref only for a task that explicitly asks about a past state, and keep it task-local.
- When a user supplies an approved scope manifest, use it only to establish the intended repository set; treat listed package paths as candidates and verify each against current source before publishing. Do not expose private manifest details or carry historical version pins into reusable guidance. A static inventory is not the active Flow runtime registry and does not replace source inspection.

## Official references

- [srcpack README and selector/profile guide](https://github.com/nexssp/srcpack/blob/main/README.md)
- [srcpack bundled cookbook](https://github.com/nexssp/srcpack/blob/main/bundles/system/cookbook.md)
- [srcpack remote-source details](https://github.com/nexssp/srcpack/blob/main/docs/REMOTE_SOURCES.md)
- [Flow CLI reference](https://github.com/nexssp/flow/blob/main/docs/cli.md)
- [Flow language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md)
- [Flow extension guide](https://github.com/nexssp/flow/blob/main/docs/extensions.md)
- [Kernel README](https://github.com/nexssp/kernel/blob/main/README.md)
