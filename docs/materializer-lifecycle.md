# Extension materialization order

## Contract

`Bundle.MaterializePhase` declares an extension's materialization phase. The
runner executes `compiler.MaterializeDeclarations` first, then
`compiler.MaterializeCompositions`, preserving the supplied bundle order within
each phase. The zero value means declarations, so existing extensions need not
change their declarations. An unsupported phase is a bundle validation error,
even when the bundle has no materializer.

The declaration phase mounts actions from metadata. The composition phase
compiles declarations that refer to those actions. The `pipeline` extension
declares its own composition phase; the runner does not recognize its ID.
Ordering does not depend on `Bundle.Alias`, and aliases are still passed to
materializers unchanged. A bundle without a materializer adds no execution
step.

## Why this contract

Previously, the runner moved the bundle with ID `pipeline` to the end. That
placed knowledge about one language feature in the execution layer. An
extension adapter alone cannot establish ordering relative to other bundles'
materializers; `runner.BuildConfig` owns that ordering.

The contract remains in Flow's existing extension microkernel
(`compiler.Bundle`); the runner only executes it. No Kernel mechanism is needed:
this is ordering of Flow compilation stages, not a new action-execution
primitive. Kernel's DAG serves action graphs and is not needed to stably order
these two phases.

Alternatives considered: a bundle-name special case violates extension
boundaries; manually ordering the native catalog does not cover external
bundles; arbitrary numeric priorities do not express intent; and a full
before/after dependency graph is more contract than this stage needs. The
runner uses stable sorting from the standard library, not a custom topological
sort.

## Boundaries and compatibility

This is a phase-ordering contract, not a dependency graph between extensions
and not a guarantee that dependencies within one phase will be resolved. The
`pipeline` extension continues to resolve its internal sub-pipeline
dependencies. Directive, hook, library, preprocessing, and wrapper order is
unchanged. The `Materializer` signature and `MaterializeReq` structure are
unchanged.

Adding a field to public `Bundle` preserves existing keyed literals; external
unkeyed literals require updating. Account for this source-compatibility
constraint when releasing. New phase values require an explicit contract
extension and validation.

## Verification

Tests cover phase stability, ID neutrality (including the ID `pipeline`),
alias and request preservation, no mutation of input bundles, empty
materializers, error propagation, phase validation, and integration of a real
pipeline declaration with actions supplied by another bundle. Existing
sub-pipeline dependency and cycle tests remain in place.
