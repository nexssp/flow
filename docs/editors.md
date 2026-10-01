# Editor setup: VS Code and Zed

The supported editor integration is a manually invoked task that runs the real CLI command, `nflow lint <file.nflow>`. First install `nflow` (for example, with `go install github.com/nexssp/flow/cmd/nflow@main`) and make sure it is available on the editor's task `PATH`. Run the task from the repository workspace so relative `@include` and `@require` paths resolve as expected.

## VS Code

Create `.vscode/tasks.json` in the repository:

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "nflow: lint current file",
      "type": "process",
      "command": "nflow",
      "args": ["lint", "${file}"],
      "options": { "cwd": "${workspaceFolder}" },
      "presentation": { "reveal": "always", "panel": "dedicated" }
    }
  ]
}
```

Open a `.nflow` file, then select **Terminal → Run Task → nflow: lint current file** (or use the Command Palette and choose **Tasks: Run Task**). This uses a process task and a separate argument for `${file}`, so the active file path is not interpolated into a shell command. VS Code documents workspace tasks in [`tasks.json`](https://code.visualstudio.com/docs/debugtest/tasks) and the `${file}` / `${workspaceFolder}` variables in its [variables reference](https://code.visualstudio.com/docs/reference/variables-reference).

## Zed

Create `.zed/tasks.json` in the repository:

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

Open a `.nflow` file, invoke **task: spawn**, and choose **nflow: lint current file**. Zed documents project-specific tasks in `.zed/tasks.json`, the `command` / `args` / `cwd` fields, and `$ZED_FILE` / `$ZED_WORKTREE_ROOT` in its [Tasks guide](https://zed.dev/docs/tasks). Keeping the path as one `args` element follows Zed's documented quoting guidance for paths containing spaces.

## What this provides—and what it does not

These configurations run lint only when you explicitly invoke the task. The current CLI does not expose an LSP or an on-save diagnostic service, so neither snippet runs automatically on save or publishes diagnostics into the editor's Problems panel. Lint output is shown in the task terminal; it checks preprocessing, parsing, and known atom/modifier names, not runtime input values or security properties. To check the user-facing examples, run `nflow lint ./examples/...`. `nflow lint ./...` also discovers internal developer sources and extension fixtures, some of which currently produce context-specific diagnostics (see the [CLI guide](cli.md)); `task examples` separately executes the curated runnable flows.
