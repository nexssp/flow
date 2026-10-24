# Bundle source modules

A `compiler.Bundle` can ship reusable `.nflow` modules beside its Go actions. The source stays in the bundle's own `fs.FS`; consumers select an exported module with `@include` instead of copying it into their application directory.

## Bundle API

Use `SourceModules` for the filesystem and list every public source entry in `ExportedModules`:

```go
compiler.Bundle{
    ID:             "si-ai",
    Libraries:      []action.Library{aiLibrary},
    SourceModules:  sourceFiles,
    ExportedModules: []string{"nflows/lib/critic.nflow"},
}
```

Export paths are slash-separated paths rooted at `SourceModules`, must name existing `.nflow` files, and cannot contain traversal. Paths omitted from `ExportedModules` remain private, even if the filesystem contains them. A bundle with Go actions only can leave both fields unset.

## Importing a module

The source qualifier is the local alias from `@require` (or a simple bundle ID when no alias is specified). The path after the colon is looked up in that bundle's exported-module list and filesystem:

```nflow
@require github.com/nexssp/si-ai v1.2.3 as ai
@include ai:nflows/lib/critic.nflow AS critic

critic.pipeline.si_ai_critic
```

`AS critic` has the same meaning as for a file include: the module's declarations stay in a separate source scope, and its exported pipeline is available under `critic.pipeline.*`. Without `AS`, the module filename stem remains the include namespace. The `ai` qualifier only selects the required bundle; it does not replace the include namespace.

The imported module is preprocessed and compiled at build/compile time. Its actions are the Go actions from the same resolved bundle instance, so the module and capabilities share one package version. This does not add a runtime source loader or change Kernel behavior.

Ordinary filesystem includes such as `@include "./local/child.nflow"` continue to resolve relative to the including file. Relative includes made by a bundled module resolve within that bundle, including private implementation files; consumers still cannot name those private files directly with `bundle:path`. An unknown or ambiguous qualifier, a missing required bundle, an invalid path, or a direct include of a private module produces a compile-time error.
