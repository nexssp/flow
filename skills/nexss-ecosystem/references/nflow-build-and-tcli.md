# NFlow build and TCLI entrypoint

Use one `.nflow` entrypoint as the source of truth for workflow behavior and, when needed, its command-line interface. Transport's TCLI Flow bundle adapts command-line input to NFlow pipeline bindings; `nflow build` packages that same flow as an executable. A separate handwritten TCLI/Go dispatcher is not the default architecture.

## Keep the responsibilities distinct

| Layer | Responsibility | Avoid confusing it with |
|---|---|---|
| NFlow entrypoint (for example, `migrate.nflow`) | Workflow steps, migration policy, and composition of capabilities | A place that must be replaced by a wrapper to get a CLI |
| Transport TCLI Flow bundle | Command binding, command/flag/positional argument adaptation, generated help, and listening for CLI invocation | A second implementation of the migration/workflow |
| `nflow build` | Building a self-contained executable embedding the `.nflow` source and required `@require` modules | A command that requires a separate user-written launcher |

Keep the actual migration or other application behavior in NFlow and existing reusable actions. TCLI supplies the CLI interface; it does not require that the workflow itself be reimplemented in Go.

## Bind a command to an NFlow pipeline

The Transport README documents this Flow pattern:

```nflow
@require github.com/nexssp/transport/tcli/nexssflow
@pipeline hello:cli="hello:Greet someone"
  const @{ value: "Hello, Julia" }
@end
tcli.listen
```

For a migration CLI, use the same shape in `migrate.nflow`: require the TCLI Flow bundle, bind a pipeline to a command with the `:cli="command:description"` pipeline binding, and include `tcli.listen` in that same entrypoint. Put the migration steps in the bound pipeline (and compose them with existing NFlow capabilities/packages). TCLI maps command-line commands, flags, and positional arguments, generates help from the binding metadata, and uses the process arguments by default. Check the current Transport docs and bundle version for exact argument/request-field controls; some advanced CLI controls may remain Go-only.

This is an adapter relationship: TCLI dispatches into the bound NFlow pipeline. It is not a direction to write a separate Go `main` that owns the migration behavior or to split the workflow into a second source of truth.

## Build that same source as an executable

```sh
# Build for the current host platform.
nflow build -o ./dist/migrate migrate.nflow

# Example: cross-compile a Windows/amd64 executable.
GOOS=windows GOARCH=amd64 nflow build -o ./dist/migrate.exe migrate.nflow
```

`nflow build` embeds the workflow and its `@require` modules. When launched, the binary runs the flow and accepts the same JSON payload and CLI flags as `nflow run`. The build defaults to the host platform; set `GOOS` and `GOARCH` for a cross-build. Choose the output suffix for the target platform (`.exe` for Windows).

“Self-contained” here means the workflow source and required Go modules are embedded in the executable. It does not remove runtime requirements such as external services, credentials, data files, or platform-specific configuration; provide those through the deployment environment using the application's normal configuration mechanism.

## Verify the actual command surface

Check the current CLI and host rather than inferring behavior from a package name:

1. Confirm the `@require` resolves for the Flow build and target environment.
2. Run `nflow lint migrate.nflow` to check source and the active registry. Lint is not a runtime test.
3. Build the same entrypoint, then inspect help and exercise a safe command from the generated executable. Use a dry-run or fixture only if the workflow actually supports it; do not assume one exists.
4. Test real migration side effects only against an explicitly safe target and with the intended credentials/configuration.

NFlow build behavior and flags are documented in the [Flow CLI reference](https://github.com/nexssp/flow/blob/main/docs/cli.md#build-a-standalone-executable). The TCLI Flow binding and listener pattern are shown in the [Transport README](https://github.com/nexssp/transport/blob/main/README.md#nexss-flow-integration). Recheck those sources and the resolved bundle before relying on exact behavior in a moving checkout.
