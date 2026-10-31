# Nexss Flow documentation

This folder documents Flow `v0.59.0` as a domainless orchestration language. Flow provides the syntax, compiler, runner, schemas, policies, composition, and lifecycle. Domain capabilities are added through mounted bundles or `@require` modules.

## Start here

1. [Language overview](language.md) — quick syntax walkthrough.
2. [Syntax reference](language-reference.md) — authoritative implementation-backed language and runtime reference.
3. [Syntax cheatsheet](syntax-cheatsheet.md) — compact daily reference.
4. [Cookbook](cookbook.md) — practical patterns and working recipes.
5. [CLI](cli.md) — run, lint, format, test, inspect, build, serve, and package flows.

## Core language

- [Symbols](symbols.md) — local namespaces, bare values, typed values, and scope.
- [Schemas](schema.md) — declarations, request/response boundaries, strictness, and defaults.
- [Runtime validation](validation.md) — validation behavior and boundaries.
- [Modifiers](modifiers.md) — timeouts, retries, concurrency, metadata, authorization, and policy syntax.
- [Error handling](on-error.md) — fallback, typed recovery, reraising, and cancellation boundaries.
- [Retry](retry.md) and [resilience](resilience.md) — retry and optional resilience policies.
- [Compile-time constants](constants-compile-runtime.md) — `@const`, `@const.load`, and runtime values.

## Runtime and operations

- [Requirements and `@require`](requirements.md) — external bundle loading and harness behavior.
- [Extension model](extensions.md) — actions, directives, operators, modifiers, schemas, hooks, and bundle contracts.
- [Durable execution](runner-durability.md) — journals, execution IDs, replay, idempotency, and resume.
- [Approval pause/resume](approval-resume.md) — durable approval gates.
- [Environment](environment.md) — CLI environment inspection.
- [Editors](editors.md) — VS Code and Zed integration.
- [Releases](releases.md) — CLI release and distribution information.
- [Long-lived services](long-lived-services.md) — service/serve operation.

## Specialized capabilities

- [Static embedding](static-embed.md) — optional embedded files.
- [Templates](template.md) — optional template rendering.
- [Stream folds](stream-fold.md) — stream aggregation.
- [Bundle source modules](bundle-source-modules.md) — exported `.nflow` modules from bundles.

## Documentation rules

The language reference is the authority for supported syntax. The cheatsheet and cookbook should show the shortest valid user-facing spelling: use `const`, `noop`, and other native keywords rather than canonical internal IDs such as `runtime.const`, except when explaining compilation, catalogs, traces, or action names.

A capability is not available merely because an extension exists in a source tree. Confirm the active registry with `nflow catalog --flow FILE`, or load an external bundle explicitly with `@require`.
