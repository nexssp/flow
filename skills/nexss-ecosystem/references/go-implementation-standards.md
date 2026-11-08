# Go implementation standards

Read this reference when proposing, reviewing, or changing Go code in the Nexss ecosystem. Treat repository instructions, supported Go toolchains, existing architecture, and configured checks as authoritative. These standards guide decisions; they do not override correctness, compatibility, or explicit project conventions.

## Core approach

- Prefer truthful, source-backed answers and working, idiomatic Go. Verify APIs, behavior, and toolchain support against the current checkout and its tests; do not invent interfaces or assume the newest API is available.
- Keep solutions simple, maintainable, and appropriately scoped. Prefer deleting unnecessary code and reducing API surface when that preserves behavior and clarity.
- Before adding a mechanism, ask whether the need can be removed or met by an existing capability. Fix root causes rather than layering on workarounds.
- Reuse the Go standard library and existing project utilities before writing equivalent helpers. Check the actual toolchain before using newer standard-library packages or APIs (for example, `slices` or `maps`). Avoid custom implementations when a suitable, supported, well-maintained solution already exists.
- Keep business logic and orchestration in their appropriate layers. For Nexss Flow work, use NFlow for workflow composition; add Go only for the smallest behavior that cannot reasonably be expressed through existing Flow capabilities or libraries.

## Performance and allocations

- Avoid unnecessary allocations, copies, conversions, and pointer indirection. Prefer bounded, demand-driven data structures where they fit the workload.
- For measured hot paths, target zero heap allocations when practical. Establish the hot path with profiling or benchmarks, inspect allocation data (for example, `go test -bench . -benchmem`), and compare before and after changes.
- Do not promise or require zero allocations everywhere. Favor correctness, clear ownership, and maintainable interfaces over micro-optimizations without evidence. Explain unavoidable allocations at API, I/O, serialization, or ownership boundaries when they matter.
- Preallocate only when expected size or capacity is known and doing so is useful. Avoid slice shifting or repeated copying in performance-sensitive queues; consider a ring buffer only when it is a justified fit.
- Do not select atomics, lock-free algorithms, or low-level layouts by default. Minimize contention and critical-section size, then benchmark representative workloads before adding complexity.
- Consider CPU and memory locality when data structures are demonstrably performance-sensitive; retain simple idiomatic representations otherwise.

## Correctness, resource ownership, and concurrency

- Bound memory, queues, retries, and other accumulating work when unbounded growth is not an explicit requirement.
- Make context cancellation, goroutine termination, and resource cleanup explicit. Avoid goroutine leaks, unnecessary background workers or tickers, and lifecycle state that can be replaced by demand-driven work. Follow external I/O and host lifecycle requirements where cleanup is necessary.
- Use synchronization that matches the sharing and consistency requirements. Do not replace a mutex with atomics or a lock-free design without evidence that it improves the actual workload and remains correct.
- Make side effects, retryability, and idempotency explicit. Do not retry operations that may duplicate effects unless the operation or provider protects against duplication.
- Avoid panics in ordinary control flow and in functions handling untrusted or external input. Check errors; wrap them with useful context when crossing an abstraction boundary. Do not silently discard errors unless the behavior is intentional and documented by the surrounding API.
- Guard numeric conversions when truncation, overflow, or sign changes could affect correctness; otherwise keep the code simple and make the safety rationale clear.

## Go style and structure

- Follow the repository's Go version, formatting, naming, package layout, and established conventions. Prefer standard Go idioms over rigid rules that conflict with the codebase.
- Use clear, descriptive names. Avoid cryptic abbreviations when they reduce readability, while retaining conventional names such as `ctx`, `err`, and receiver names used consistently by the package.
- Keep functions cohesive and readable. Split them when doing so clarifies responsibilities or makes testing easier; do not split mechanically by line count.
- Keep related types and methods together. Add a package when it creates a meaningful boundary, enables reuse or independent evolution, or enforces `internal/` privacy—not merely to reduce file length.
- Use interfaces where they enable a real substitution, test seam, or extension boundary. Avoid speculative interfaces and unnecessary abstraction.
- Prefer zero-value usability where it makes an API simpler and safe; do not contort an API to achieve it when explicit construction is materially clearer.
- Use comma-ok type assertions when failure is possible and must be handled. Use exhaustive or default switch cases according to the type, domain, and configured lint rules; do not add meaningless defaults that conceal newly added cases.
- Comments should be concise, in English for shared Nexss source, and explain non-obvious intent or constraints rather than restating code. Preserve existing useful comments; do not add organizational commentary or leave temporary scaffolding behind.

## Tests, benchmarks, and static checks

- Prefer the repository's established test helpers, including `xtest` or `ktest` where applicable. Avoid test-only production hooks or hacks.
- Make concurrency tests deterministic with channels, barriers, or other explicit coordination. Avoid sleeps as synchronization; use bounded waits only as failure timeouts where necessary.
- Test behavior and boundaries, including errors, cancellation, resource cleanup, and side effects relevant to the change.
- Add or run benchmarks when making performance claims. Keep allocation assertions focused on stable, important hot paths; exact zero-allocation checks can be brittle across toolchain and implementation changes.
- For each change, format touched files first, then run focused lint/static checks and the narrowest relevant tests. Do not run every repository test for each edit; broaden verification for cross-package changes, risky behavior, pull requests, releases, or when repository policy requires it.
- Run the repository's configured formatter, linter, and tests rather than assuming a universal command. Inspect configuration and report the exact checks run and their outcomes. If a check cannot run, state why; do not imply it passed.
- Review changes for correctness, unintended scope, error handling, compatibility, and unnecessary complexity before presenting them.

## Environment and change presentation

- Detect the actual operating system, shell, repository instructions, and available tools. Use the user's stated environment preferences when applicable; do not assume Windows, PowerShell, WSL, or a particular search utility for every task.
- For Windows 11 workflows, prefer PowerShell (`pwsh`), use `rg` for search instead of `grep`, and run Docker through WSL when those tools are available. Otherwise follow the actual task environment and project conventions.
- Check compatibility before recommending tool, language, or dependency upgrades. Do not change versions or dependencies merely to reach the latest release unless requested or required; explain material compatibility tradeoffs.
- Preserve unrelated code and comments. Avoid broad restructuring solely for cosmetic diff cleanliness.
- When showing code changes, identify the file path and use the format requested by the user or appropriate to the task. For targeted edits, provide a precise patch or exact, searchable replacement; for larger changes, provide the relevant complete function or file. Follow repository tooling for import management or generation rather than inventing a separate workflow.
- Keep responses concise but complete; give rationale and evidence, not hidden internal deliberation. Expand into a step-by-step explanation when the user asks for one.
- Do not create commits unless requested. When asked, follow the repository's commit convention, stage only the intended changes, and show the exact commands when that helps the user review the action.
- Never claim to have run a command, benchmark, test, or linter unless it was actually run. Summarize verified outcomes and remaining uncertainty concisely.

## Applying these standards

Use these guidelines proportionately. For a small change, favor a focused edit and focused test. For architecture or performance work, first map the current design, identify the real constraint, establish evidence, then choose the least complex solution that meets it. State assumptions and distinguish measured results from expectations.
