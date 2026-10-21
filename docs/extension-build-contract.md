# Extension-owned metadata and build planning

## Missing contracts

The directive table knows which directives are active, but Include previously
maintained a second, closed list of other extensions' metadata keys. New
extensions could preprocess correctly yet lose declarations on the second
include. A SourceLoader observes reads, not directive execution or the resulting
metadata. Replacing it to collect raw text also overrides a caller's loader.

The build generator maintained another native bundle catalog and substring
matchers. This could omit shipped features, select features mentioned only in
comments, or silently expose optional self-test features in ordinary flows.

These are DSL composition concerns in Flow's compiler microkernel, not
application-independent runtime behavior. Existing Kernel action lifecycle hooks
cannot represent directive metadata ownership or preprocessing source fragments.
No Kernel runtime change or second registry is needed.

## Metadata ownership

A directive can contribute one MetadataMerger with an explicit list of keys.
The existing DirectiveTable validates ownership and composes active mergers.
Include delegates to that table; it does not import or name another extension.
Typed append is reusable infrastructure; domain-specific duplicate and
replacement rules stay in their owning extension. Coupled pipeline body,
modifier and origin maps are merged as one record.

Mergers use copy-on-write for nested values. The table commits its temporary
metadata map only after successful merging. AfterMerge can update the caller's
remaining source lines; these callbacks must validate before mutating lines.
Constants owns its own post-include substitution. Unknown, unowned metadata
retains the existing parent-wins behavior for compatibility.

## Preprocess lifecycle

WithPreprocessObserver observes successful fragments synchronously after their
children. It composes with an existing observer and leaves SourceLoader intact.
Events carry the original source, substituted directive input, cleaned executable
body, metadata and actual successful directive invocations. A diamond is evaluated
per include edge; it is not a cycle. Cancellation is checked before preprocessing,
between directives and before reporting success.

EmbedSource composes cleaned fragments without destroying line boundaries. The
compiler retains outer-to-inner source ancestry for every rendered line. Scope
owns its span-merging rule and matches against that ancestry, preserving both
enclosing policies and following source line numbers through nested includes.

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

No source flattening is performed for Include: quoted whitespace and comment
boundaries remain intact. Existing cache fingerprint changes and local-module
root selection are preserved. Full changed files are delivered in stages; no
workspace files or repository-wide lint settings are replaced.

External metadata collections must declare their own merger. The former llms
and sandboxes special cases have no owning extensions in the supplied tree;
those external owners are not migrated here. Unowned values remain parent-wins,
not an implicit append policy. Optional cov usage needs an explicit selftestkit
requirement; resilience is likewise selected explicitly.
