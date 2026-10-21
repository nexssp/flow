# Source modules and build planning

## Missing contracts

`@include` is a source-based module import. The compiler loads and preprocesses
the imported source recursively, preserving its own declaration state and
resolver instead of merging child metadata into the parent. The include
extension parses the path and optional namespace alias; it does not maintain a
closed list of metadata keys owned by other extensions. Source modules can be
reused with different aliases, and each nested import has its own scope.

The build generator maintained another native bundle catalog and substring
matchers. This could omit shipped features, select features mentioned only in
comments, or silently expose optional self-test features in ordinary flows.

These are DSL composition concerns in Flow's compiler microkernel, not
application-independent runtime behavior. Imported clean bodies are compiled
as module bodies and invoked at their import site rather than textually pasted
into parent source. A declaration-only import does not add an execution atom.
The module exposes qualified capabilities and symbols; declarations such as
constants, profiles, macros and schemas remain local. Duplicate declaration
errors identify both source locations. `@require` remains separate and is not
changed by source imports.

## Import and declaration scope

Each parsed source owns its declarations. Pipeline references inside an imported
pipeline are resolved against that module, including its local pipelines and
nested imports; parent references continue to use the parent resolver. Exported
pipelines and capabilities are qualified by the alias (or filename stem by
default), for example `billing.pipeline.charge`. An imported symbol namespace
is likewise qualified, as in `billing.queues.review`; it does not become an
unqualified parent namespace. Imported macros and schemas are not promised for
use in parent modifiers or declarations.

The compiler owns source loading, cycle detection, source positions, duplicate
declaration diagnostics and module resolution. There is no `MetadataMerger` or
special-case extension metadata whitelist for include. A repeated name in one
module is rejected with both declaration locations; same-named locals in
separate modules are isolated.

## Build ownership

Native remains the only shipped preset. Planning obtains bundle contributions
from that preset, rather than maintaining another list in builder. The compiler
matches its lexer tokens against actual registered action, source, operator,
primary and modifier contributions. An extension declares opaque advisor
modifiers and dependency/replacement relations; builder does not know feature
names. Optional bundles require an explicit declaration. All-bundles means all
standard bundles, not the separate self-test environment. The runner validates
all native keyword targets at boot. Mandatory providers are therefore derived
from the existing KeywordMappings table and actual action registrations, never
from a second hard-coded runtime bundle list.

This stage deliberately uses conservative token-level linking, not a claim of a
minimal AST-resolved dependency graph. It can retain extra packages for tokens in
unused declarations. It must not be used as a semantic validator; the generated
runner still performs normal compilation, resolution and validation. Eliminating
all such over-linking would require extension-aware AST reference reporting and
is not simulated by substring matching or a builder-only parser.

## Compatibility

Imported files remain source modules rather than being flattened into parent
source; source locations and nested module boundaries remain significant.
Optional `cov` usage needs an explicit selftestkit requirement; resilience is
likewise selected explicitly.
