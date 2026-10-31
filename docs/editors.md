# Editor and IDE support

The official editor integration is maintained in [nexssp/flow-editor-support](https://github.com/nexssp/flow-editor-support). It supports `.nflow` files through the Flow core language server and formatter.

Install Flow `v0.59.0` or newer for this documentation set:

```sh
go install github.com/nexssp/flow/cmd/nflow@v0.59.0
nflow --version
```

The editor-support launcher requires Flow `v0.44.2` or newer. The current Flow release is recommended because it keeps the editor features aligned with the language and compiler used by the project.

## Supported editors

| Editor | Syntax highlighting | Diagnostics | Completion | Hover | Formatting | Integration level |
|---|---:|---:|---:|---:|---:|---|
| Visual Studio Code | Yes | Yes | Yes | Yes | Yes | Native Flow LSP client |
| Zed | Yes | Yes | Yes | Yes | Yes | Native Flow LSP client |
| Claude Code | Host editor | Yes | Yes | Yes | Manual `/flowfmt` | LSP plugin |
| Notepad++ | Yes | No | No | No | NppExec command | UDL and formatter command |
| JetBrains IDEs | Yes | No | No | No | External Tool or File Watcher | TextMate bundle and formatter command |

The LSP provides document diagnostics, completion, hover, and formatting. Project-wide compilation and external `@require` checks still use `nflow lint`.

## Executable selection

The shared launcher selects the Flow executable in this order:

1. `NFLOW_BIN`
2. `FLOW_NFLOW_LSP_BIN`
3. `FLOW_NFLOW_BIN`
4. `nflow` on `PATH`

Use `NFLOW_BIN` as the normal override:

```sh
export NFLOW_BIN="$HOME/bin/nflow"
nflow --version
```

The launcher checks the selected executable version before starting `nflow lsp`. A missing executable or incompatible version is reported instead of silently disabling language features.

## Visual Studio Code

The VS Code package is `vscode/` in the editor-support repository. Install the published VSIX, or package that directory with its pinned dependencies. The extension identifier is `nexssp.nexss-flow-syntax`.

The extension activates for `.nflow` files and provides:

- native Flow diagnostics;
- completion snippets;
- hover information;
- Format Document through `nflow fmt`;
- optional format-on-save;
- commands for language-server output, LSP tracing, and the selected Flow executable.

If `nflow` is not on the editor's `PATH`, set the executable path in VS Code settings:

```json
{
  "nexssFlow.nflowCommand": "/absolute/path/to/nflow"
}
```

Leave `nexssFlow.languageServer.command` empty to use the official launcher and its version check. A custom language-server command is an advanced override and bypasses that check.

To format Flow files on save:

```json
{
  "[nexss-flow]": {
    "editor.defaultFormatter": "nexssp.nexss-flow-syntax",
    "editor.formatOnSave": true
  }
}
```

For project-wide validation, run:

```sh
nflow lint ./flows
```

## Zed

The repository contains two Zed packages:

- `nexss-flow-zed.zip` for current Zed;
- `nexss-flow-zed-legacy-0.230.2.zip` for Zed `0.230.2`.

Extract the appropriate archive and select the directory containing `extension.toml` through **Install Dev Extension**.

The current Zed package provides syntax highlighting and the native Flow language server. Its `extension.toml` uses the `nexss-flow-language-server` configuration and the shared launcher.

Set `NFLOW_BIN` in the environment inherited by Zed when the executable is not on `PATH`:

```sh
export NFLOW_BIN="$HOME/bin/nflow"
```

Enable format-on-save in Zed settings:

```json
{
  "languages": {
    "Nexss Flow": {
      "formatter": "language_server",
      "format_on_save": "on",
      "tab_size": 2
    }
  }
}
```

Zed can also run the Flow CLI as a project task when a complete project check is needed:

```json
[
  {
    "label": "nflow: lint current file",
    "command": "nflow",
    "args": ["lint", "$ZED_FILE"],
    "cwd": "$ZED_WORKTREE_ROOT",
    "save": "current",
    "use_new_terminal": false,
    "reveal": "always",
    "show_command": true
  }
]
```

Save this as `.zed/tasks.json` and invoke **task: spawn**.

## Claude Code

Install the `claude/` plugin from the editor-support repository and enable it for the project. The plugin starts the shared `lsp/nflow-lsp.js` launcher and connects Claude Code to `nflow lsp`.

The plugin provides diagnostics and code intelligence for `.nflow` files. It keeps `/flowfmt` as the explicit formatting command and forwards formatting to the core CLI:

```text
/flowfmt
```

The plugin does not reimplement Flow parsing or formatting. Use `nflow lint` for checks that depend on external bundles or project-wide capability resolution.

## Notepad++

Import `notepadpp/flow-udl.xml` through **Language → Define your language → Import** and associate `.nflow` files with the imported Nexss Flow language.

The User Defined Language profile provides colorization only. It does not provide LSP diagnostics or compiler validation.

For explicit formatting:

1. Install NppExec.
2. Put `nflow` on `PATH`, or set `NFLOW_BIN`.
3. Copy `notepadpp/flowfmt.cmd` to the Notepad++ plugins configuration directory.
4. Run `notepadpp/nppexec/Format-Flow.npes`.

The formatter saves, formats, and reopens the current file. Invalid source is not rewritten. Run the project check separately:

```sh
nflow lint path/to/flow.nflow
```

## JetBrains IDEs

The editor-support repository includes a TextMate bundle under `jetbrains/textmate`. Add it through **Settings/Preferences → Editor → TextMate Bundles** and associate `*.nflow` files if required.

The TextMate bundle provides syntax highlighting. This package does not provide a native JetBrains LSP or PSI inspection provider.

For manual formatting, create an External Tool with:

```text
Program: nflow
Arguments: fmt --write "$FilePath$"
```

A File Watcher can invoke the same command on save. The selected Program controls which Flow executable is used; `NFLOW_BIN` does not override an explicit External Tool path.

Run full validation separately:

```sh
nflow lint path/to/flow.nflow
```

## Common formatting and validation commands

```sh
nflow fmt flow.nflow
nflow fmt --write flow.nflow
nflow fmt --check flow.nflow
cat flow.nflow | nflow fmt --stdin
nflow lint flow.nflow
nflow catalog --flow flow.nflow
nflow show flow.nflow
```

`nflow fmt` is the single formatting implementation used by the editor integrations. `nflow lint` performs preprocessing, parsing, registry, and compile-time checks. Neither command evaluates arbitrary runtime input or proves external service behavior.

## LSP and project-check boundaries

The LSP analyzes the open document. A bundle required by `@require` but not linked into the running Flow executable may need a project `nflow lint` instead of relying only on the editor diagnostics.

Use the LSP for interactive document feedback and `nflow lint` for the authoritative project check. Use `nflow fmt --write` for explicit, repeatable formatting in CI or editor automation.

## Official source

The editor packages, launcher, grammars, snippets, formatter adapters, and installation scripts are maintained at [github.com/nexssp/flow-editor-support](https://github.com/nexssp/flow-editor-support).
