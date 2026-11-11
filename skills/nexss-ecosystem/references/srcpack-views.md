# Nexss srcpack view catalogue

Use this reference when a task crosses Nexss repositories or needs reusable context for REST/HTTP, AI, sandbox, storage, transports, testing, language runners, or tooling.

## Contents

- [How to compose views](#how-to-compose-views)
- [Remote selector syntax](#remote-selector-syntax)
- [Available views](#available-views)
- [Task recipes](#task-recipes)
- [Profiles, tests, and token control](#profiles-tests-and-token-control)
- [Validation and evidence](#validation-and-evidence)

## How to compose views

The Flow checkout's `nexss.srcpack.yml` stores **flat selector sets**. Compose several saved views by passing their names as separate positional arguments; each top-level view argument expands to its selectors. A saved view cannot include another view by name—the installed selector resolver expands views once, so a nested name would be treated as a path. Keep the views flat and compose them at invocation time.

The file keeps a small `nexss-core` baseline and separate domain add-ons. Add only views relevant to the task, then append the current local package directory with `#with-tests` when behavioral context is needed. The optional `flow-compiler-map` is separate from `nexss-core` so compiler structure is not included in every task.

Directory selection is recursive. If a view selects a repository root and also names child directories, the same files can appear twice. Use `~exclude` on the parent selector before adding child selectors; the config uses this for Sandbox, Testkit, Observability, and Repo. Avoid combining views that bring in overlapping domains unless both are needed.

If srcpack is run outside the Flow workspace where this config is discovered, pass the config explicitly with `--config /path/to/nexss.srcpack.yml` (see `srcpack --help`). Views are project configuration, not global skill settings.

## Remote selector syntax

A remote source uses srcpack's selector grammar, not a GitHub URL or Go import path:

```text
github:OWNER/REPO[//PATH][?ref=REF][#PROFILE]
```

For example, `github:nexssp/transportai//ta2a#api` means the public `nexssp/transportai` repository, its `ta2a` subdirectory, rendered with the built-in `api` profile. `github:` is the required remote-source prefix; `//` introduces a repository-relative subpath; `?ref=` optionally selects a branch, tag, or commit. Without a ref, srcpack follows that repository's current default revision. `github.com/nexssp/transportai` and `https://github.com/nexssp/transportai` are not remote selectors and are treated as local paths.

Remote repositories must be public and Git must be installed. Private-repository credentials are not forwarded. Quote selectors in shell commands, especially when they contain `#`, `~`, `@`, or line ranges.

## Available views

| View | Use it for | Verified source areas |
|---|---|---|
| `flow-compiler-map` | Optional local compiler architecture map for compiler-focused work. | Flow `compiler#dense` |
| `nexss-core` | Small architecture baseline; combine with one relevant domain view. | [Kernel](https://github.com/nexssp/kernel): action API entrypoint and DAG; [Flow](https://github.com/nexssp/flow): runner entrypoint |
| `rest-api` | HTTP action/client contracts and the Flow HTTP source. | Flow `extensions/external/http.go`; [Transport](https://github.com/nexssp/transport) `thttp` |
| `terminal-cli` | CLI/terminal integration when that surface matters. | Transport `tcli`; Flow's `internal/terminal` TTY-detection helper |
| `a2a-mcp-integration` | Focused review of A2A/MCP public contracts and their Transport AI Flow bundle facade. | [Transport AI](https://github.com/nexssp/transportai) `ta2a`, `tmcp`, `nexssflow` |
| `ai-architecture` | Provider APIs, gateway, retrieval/memory, Flow actions, and A2A/MCP protocols. Use instead of `a2a-mcp-integration` when the broader AI stack matters. | [AI](https://github.com/nexssp/ai) and [Transport AI](https://github.com/nexssp/transportai) |
| `sandbox-runtime` | Sandbox contracts, policy, Docker/Podman backends, host tools, and Flow integration. | [Sandbox](https://github.com/nexssp/sandbox) |
| `storage-budget` | Storage/blob backends, Flow actions, and budget reservation/settlement. | [Storage](https://github.com/nexssp/storage), [Cost](https://github.com/nexssp/cost) |
| `transport-protocols` | Cross-protocol contracts for HTTP, CLI, A2A, MCP, and NATS. | [Transport](https://github.com/nexssp/transport), [Transport AI](https://github.com/nexssp/transportai), [Transport NATS](https://github.com/nexssp/transportnats) |
| `nats-and-testkit` | NATS/JetStream Flow actions and HTTP/SSE/RPC/chaos testing helpers. | [Transport NATS](https://github.com/nexssp/transportnats), [Testkit](https://github.com/nexssp/testkit) |
| `test-observability` | Test contracts, validation hooks, metrics/tracing, and Flow telemetry. | [Testkit](https://github.com/nexssp/testkit), [Validation](https://github.com/nexssp/validation), [Observability](https://github.com/nexssp/observability) |
| `language-and-ops` | Language runners, Flow process integration, source analysis, and backend generation. | [Lang](https://github.com/nexssp/lang), [OPS](https://github.com/nexssp/ops) |
| `srcpack-and-repo` | Source-pack resolution/GitHub acquisition and repository/GitHub inspection. | [srcpack](https://github.com/nexssp/srcpack), [Repo](https://github.com/nexssp/repo) |

The `terminal-cli` view does **not** imply a separate `nexssp/terminal` repository: public checks did not find one. The canonical CLI action adapter is `transport/tcli`; `flow/internal/terminal` only detects terminal capabilities. Likewise, no separate public `nexssp/restapi` repository was found; use `transport/thttp` for HTTP route/handler contracts. Flow's `cli/serve.go` is not a production HTTP server implementation.

## Task recipes

Run these from the Flow repository root. Replace `path/to/active/package` with the narrow local package directory from the current worktree.

```sh
# Real integration review: compare A2A/MCP contracts with Flow's actual bundle-loading path.
# This packages source; it does not prove that a host registers either remote bundle.
srcpack nexss-core a2a-mcp-integration "extensions/require#with-tests" \
  --prompt "Assess the Flow bundle-loading path against the A2A and MCP APIs. Separate existing support from missing Flow wiring; cite source paths and tests, and list host configuration required. Do not infer runtime registration from package presence." \
  --cost -o /tmp/flow-a2a-mcp-review.md

# HTTP adapter review with the local implementation/tests and the public transport contract.
srcpack "extensions/external#with-tests" "github:nexssp/transport//thttp#api" \
  -o /tmp/rest-context.md

# Compare protocol contracts without pulling in unrelated AI provider implementations.
srcpack nexss-core transport-protocols "path/to/active/package#with-tests" \
  -o /tmp/transport-context.md

# NATS broker work with its dedicated test helpers; use this instead of
# transport-protocols when the other protocols are not part of the task.
srcpack nexss-core nats-and-testkit "path/to/active/package#with-tests" \
  -o /tmp/nats-context.md

# AI runtime work that genuinely crosses sandbox execution, persistence, or budgets.
srcpack nexss-core ai-architecture sandbox-runtime storage-budget \
  "path/to/active/package#with-tests" -o /tmp/ai-runtime-context.md

# Cross-cutting correctness, telemetry, source tooling, or code generation.
srcpack test-observability "path/to/active/package#with-tests" -o /tmp/quality-context.md
srcpack nexss-core language-and-ops srcpack-and-repo \
  "path/to/active/package#with-tests" -o /tmp/tooling-context.md
```

For a real local path that is itself a directory, append it as an additional selector. Do not put the volatile current-work path inside a reusable saved view. Local paths resolve from the active worktree; remote `github:` selectors follow each repository's current default revision unless `?ref=` is supplied.

## Profiles, tests, and token control

A **view** chooses sources; a **profile** chooses how selected files are rendered. Use built-in profiles on each selector: `#arch` for declaration/architecture maps, `#api` for documented contracts, `#source` for implementation, and `#with-tests` for behavior. The configured defaults are overridden by the selected built-in profile: `api` keeps comments, and `with-tests` includes tests. The built-ins are usually enough; the recipes do not require a custom profile.

For test coverage, select the owning package directory with `#with-tests`, or explicitly add the test file. A single-file selector such as `file.go#with-tests` remains a single-file selection; it does not discover sibling tests. Never apply `#with-tests` to an entire repository as a routine context choice.

The selector grammar also supports exclusions, for example `github:nexssp/testkit~rpc,chaos#api`; this keeps the parent selection while excluding explicitly selected child packages. Use exclusions when a parent and child selector are both needed.

Do not assume the smallest profile always produces the smallest useful context. `compact` can exceed `dense` on a broad multi-file selection. Narrow paths first, then compare the actual file and token counts reported by `--cost`; token estimates and source contents change as upstream repositories evolve.

## Validation and evidence

After editing `nexss.srcpack.yml`, validate configuration and perform an actual pack of representative views:

```sh
srcpack config validate
srcpack view list
srcpack view show a2a-mcp-integration
srcpack nexss-core a2a-mcp-integration "extensions/require#with-tests" --plan
srcpack nexss-core a2a-mcp-integration "extensions/require#with-tests" \
  --cost -o /tmp/flow-a2a-mcp-review.md
```

`--plan` shows resolved settings but does not enumerate files or estimate final tokens. `srcpack config validate` checks config structure/profiles but does not prove every remote path exists; an actual pack performs that resolution. Inspect the output and `--cost` report before deciding whether more source is needed.

srcpack packages source; it does not execute Flow, call live REST endpoints, or register bundles into the active host. Confirm runtime behavior separately against that host's actual `@require`, registry, tests, and configuration. Review generated output before sharing it.

See [Source investigation with srcpack](source-investigation.md) for local change filters, provenance, and live-endpoint cautions. Official syntax references: [srcpack README](https://github.com/nexssp/srcpack/blob/main/README.md), [selector cheat sheet](https://github.com/nexssp/srcpack/blob/main/docs/cheatsheet.md), and [GitHub remote implementation](https://github.com/nexssp/srcpack/blob/main/bundles/github_source/remote.go).
