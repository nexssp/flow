---
name: nexss-ecosystem
description: Help users and agents discover, understand, design, implement, test, and troubleshoot the Nexss ecosystem, especially NFlow workflows, Kernel capabilities, bundles, and Go extensions. Use for Nexss onboarding, package/capability selection, NFlow syntax, source investigation, runtime behavior, or changes to Nexss code.
---

# Nexss Ecosystem

Use this skill to solve Nexss tasks from current evidence. Treat the catalogue as a discovery aid, not as proof that a package is installed, released, registered, or available in the user's host. Verify important claims against the user's checkout, current upstream sources, and the active runtime.

## Mental model

- **Kernel** supplies reusable Go execution primitives, typed actions, and runtime semantics. A Kernel API is not automatically an NFlow capability.
- **Flow / NFlow** is the language and runtime for composing registered capabilities into workflows. NFlow is domainless: its actions and features come from the active host registry and loaded bundles.
- **Bundles and adapters** expose Go capabilities to a Flow host. A repository, directory, Go package, or embedded `.nflow` file is not available to a flow unless the host loads and registers it.
- **`@include`** imports NFlow source modules; **`@require`** loads extension bundles. Do not conflate them.
- **srcpack** builds focused, provenance-preserving source context; it reads source and does not edit repositories. Use it when installed and useful; otherwise use the repository's available tools to inspect focused source and tests.

## Operating rules

1. **Orient before advising.** Identify the user's goal, repository and working tree, branch, relevant host/runtime, supported Go toolchain, and local instructions. Check the current tree and changes before editing. Ask only for missing information that materially changes the answer.
2. **Verify, don't infer.** For language behavior, confirm the current Flow reference and real `.nflow` fixtures/specs. For capability availability, inspect the target host's registry, bundle requirements, and wiring. For Go behavior, inspect the owning implementation and tests. A package name, README snippet, repository directory, or successful lint is not runtime proof.
3. **Prefer the smallest existing solution.** Compose registered NFlow capabilities first; reuse current Flow/Kernel APIs and established libraries next. Use `@func` for pure expression calculations, macros for repeated source patterns, pipelines for named workflow composition, `@include` for source modules, and `@require` for bundles. Add Go only for missing action-level behavior that cannot reasonably be composed or reused; keep orchestration and workflow policy in NFlow.
4. **Keep code concise, idiomatic, and correct.** Prefer documented short syntax where it improves clarity. Avoid duplicate helpers, unnecessary layers, packages, background workers, and APIs. Do not compress code into obscure tricks. Read [NFlow idioms](references/nflow-idioms.md) for nontrivial Flow syntax and [Go implementation standards](references/go-implementation-standards.md) for Go changes.
5. **Treat performance as evidence-based.** Avoid needless allocations and copies. Prefer zero heap allocations in measured hot paths when practical, but do not optimize blindly or make zero-allocation a universal requirement. Benchmark representative work before adding atomics, lock-free algorithms, custom buffers, or other complexity.
6. **Respect side effects and boundaries.** Before running examples or tests, identify external calls, writes, notifications, account changes, secrets, and other side effects. Protect credentials and review generated context before sharing it. Use retries only when repeating the operation is safe or idempotency is enforced by the provider.
7. **Validate focused changes first.** Format the changed source, run the smallest relevant lint/static check, then focused tests and a safe runtime fixture when behavior requires it. Do not run the full suite for every edit; reserve broad gates for pull requests, releases, broad changes, or elevated risk. Static checks do not execute actions or prove external-service behavior. For exact Flow repository commands and gates, read [Flow repository guidance](references/flow-repository-guide.md); elsewhere follow that repository's current instructions.
8. **Report evidence and uncertainty.** Give the recommended answer, current source links, exact checks run and outcomes, host/config prerequisites, and remaining uncertainty. Distinguish verified facts from inference and “not found in inspected sources” from proof that no implementation exists. Never claim an API, test result, or successful run without evidence.
9. **Avoid unnecessary churn.** Make the smallest reviewable change, preserve unrelated code and useful comments, and do not restructure for cosmetic reasons. Do not pin moving Nexss code to tags or commits in reusable guidance; use the current checkout/default branch unless the user needs a historical reproduction.

## Task workflow

1. Classify the request: onboarding, package discovery, language/syntax, capability availability, workflow/runtime design, troubleshooting, or code change.
2. Read only the relevant reference(s) below. For source work, map the repository first and then inspect the owning implementation, fixtures, and tests.
3. Verify the actual Flow surface with current CLI help and the target host's `nflow list`, `show`, `catalog`, or `explain` commands as supported. Check `@require` declarations and host wiring.
4. Choose the smallest correct layer; explain alternatives only when they affect correctness, maintainability, safety, or cost.
5. Validate proportionately and report what was—and was not—verified.

## Reference guide

- **Writing/reviewing NFlow, macros, includes, or bundle requirements:** [NFlow idioms](references/nflow-idioms.md).
- **Retries, errors, approval, replay, schemas, configuration, streams, or test/runtime boundaries:** [Flow runtime practices](references/flow-runtime-practices.md).
- **Mapping repositories, building source context, tracing undocumented behavior, or reviewing local changes:** [Source investigation with srcpack](references/source-investigation.md).
- **Package and repository discovery within the curated Nexss set:** [Package catalogue](references/package-catalogue.md). Revalidate current paths, availability, and scope before relying on it.
- **Contributing to `nexssp/flow`, adding DSL features, or choosing focused verification commands:** [Flow repository guidance](references/flow-repository-guide.md).
- **Creating or reviewing Go code, performance-sensitive code, tests, or linting:** [Go implementation standards](references/go-implementation-standards.md).

## Current source starting points

Follow links from the current repositories when paths change. Prefer official docs and implementation/tests over secondary examples.

- Flow: [README](https://github.com/nexssp/flow/blob/main/README.md), [language walkthrough](https://github.com/nexssp/flow/blob/main/docs/language.md), [language reference](https://github.com/nexssp/flow/blob/main/docs/language-reference.md), [CLI](https://github.com/nexssp/flow/blob/main/docs/cli.md), [extensions](https://github.com/nexssp/flow/blob/main/docs/extensions.md), [integrated specs](https://github.com/nexssp/flow/tree/main/spec), and installers for [Linux/macOS](https://raw.githubusercontent.com/nexssp/flow/main/install.sh) and [Windows](https://raw.githubusercontent.com/nexssp/flow/main/install.ps1).
- Kernel: [README](https://github.com/nexssp/kernel/blob/main/README.md).
- srcpack: [README](https://github.com/nexssp/srcpack/blob/main/README.md), [bundled cookbook](https://github.com/nexssp/srcpack/blob/main/bundles/system/cookbook.md), [remote-source notes](https://github.com/nexssp/srcpack/blob/main/docs/REMOTE_SOURCES.md), and installers for [Linux/macOS](https://raw.githubusercontent.com/nexssp/srcpack/main/install.sh) and [Windows](https://raw.githubusercontent.com/nexssp/srcpack/main/install.ps1). Confirm the selected release includes installer assets; older releases may not.

The catalog and this skill are reusable guidance, not a substitute for checking today's source, actual CLI, or target host.
