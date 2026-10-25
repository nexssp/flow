package cli

import (
	"fmt"
	"io"
	"sort"
)

type helpText struct {
	Summary  string
	Usage    string
	Flags    string
	Examples []string
}

var helpRegistry = map[string]helpText{
	"init": {
		Summary: "Initialize a new Nexss Flow project, or scaffold a nexssflow extension bundle.",
		Usage:   "nflow init [project_dir] [--bundle [folder]]",
		Flags:   `      --bundle      scaffold a nexssflow extension bundle instead of a project`,
		Examples: []string{
			`nflow init                      # initialize starter project in current directory`,
			`nflow init my-project           # initialize starter project in new directory`,
			`nflow init --bundle             # scaffold ./nexssflow/library.go in current repo`,
			`nflow init --bundle nexssflow_dev # scaffold custom extension folder`,
		},
	},
	"run": {
		Summary: "Run a .nflow file. Builds or reuses a harness binary when the file declares @require.",
		Usage:   "nflow run <file.nflow> [json_payload] [flags]",
		Flags: `  -v | -vv | -vvv     verbosity
  --assert=EXPR       assertion evaluated after the run
  --info              describe the pipeline instead of executing
  --journal-dir DIR   persist durable step results in DIR
  --execution-id ID   replay/resume the same run (requires --journal-dir)
  --approval TOKEN    approve named @approval gates (or all)`,
		Examples: []string{
			`nflow run flows/client.nflow '{"user_id": 42}'`,
			`nflow run flows/client.nflow --assert='result.ok == true'`,
			`nflow run flows/client.nflow --journal-dir .flow-state`,
			`nflow run flows/client.nflow --journal-dir .flow-state --execution-id invoice-2026-0042`,
			`nflow run -vv flows/client.nflow`,
			`nflow run --info flows/client.nflow`,
		},
	},
	"build": {
		Summary: "Compile a .nflow file into a standalone executable with embedded source and @require modules.",
		Usage:   "nflow build <file.nflow> [-o <output>] [--debug] [--no-trimpath] [--all-bundles]",
		Flags: `  -o, --output FILE   output binary (default: source filename)
	  --debug, -d        keep debug symbols
	  --no-trimpath      keep full source paths
	  --all-bundles      link all native extensions`,
		Examples: []string{
			`nflow build flows/client.nflow -o client.exe`,
		},
	},
	"serve": {
		Summary: "Validate a .nflow file for serve mode. The listener is not implemented in this release.",
		Usage:   "nflow serve <file.nflow> [--allow-exec]",
		Flags:   `      --allow-exec   acknowledge that external.exec/external.wasm will run host commands`,
		Examples: []string{
			`nflow serve flows/api.nflow`,
		},
	},
	"list": {
		Summary: "List every atom, modifier, directive, and operator known to the compiler.",
		Usage:   "nflow list [pattern] [--flow FILE] [--json] [--no-color]",
		Flags: `      --flow FILE   load @require bundles from FILE first
      --json        machine-readable JSON
      --no-color    disable ANSI colors`,
		Examples: []string{
			`nflow list`,
			`nflow list fs`,
			`nflow list --json > catalog.json`,
		},
	},
	"show": {
		Summary: "Show details for one atom, modifier, directive, or operator.",
		Usage:   "nflow show <name> [--flow FILE] [--no-color]",
		Examples: []string{
			`nflow show fs.walk`,
		},
	},
	"info": {
		Summary: "Describe a .nflow pipeline without executing it.",
		Usage:   "nflow info <file.nflow>",
		Examples: []string{
			`nflow info flows/client.nflow`,
		},
	},
	"lint": {
		Summary: "Run full compiler validation of .nflow sources, including @nflow_version. Builds or reuses an external harness for unlinked @require bundles when needed. Exits 1 and prints JSON issues on failure.",
		Usage:   "nflow lint <file.nflow | directory | ./...> [target...]",
		Examples: []string{
			`nflow lint flows/client.nflow`,
			`nflow lint ./...`,
			`nflow lint ./examples/...`,
		},
	},
	"explain": {
		Summary: "Show effective modifiers, the resolved AST, the built Kernel action graph, or the AST-derived plan.",
		Usage:   "nflow explain <file.nflow> [--ast|--actions|--plan] [--json|--dot]",
		Flags: `      --ast         show the effective AST
			      --actions     show the built Kernel action graph; argument values are omitted
			      --plan        show the AST-derived Flow lowering plan (not the built graph)
		      --json        emit the selected view as JSON
			      --dot         emit an action graph or plan in Graphviz DOT format`,
		Examples: []string{
			`nflow explain flows/client.nflow`,
			`nflow explain --ast flows/client.nflow`,
			`nflow explain --ast --json flows/client.nflow > ast.json`,
			`nflow explain --actions flows/client.nflow`,
			`nflow explain --plan --dot flows/client.nflow > plan.dot`,
			`nflow explain --actions --dot flows/client.nflow > actions.dot`,
		},
	},
	"expand": {
		Summary: "Show what macros in a .nflow file expand to, with definition and invocation lines.",
		Usage:   "nflow expand <file.nflow> [--macro=NAME[,NAME...]]",
		Flags:   `      --macro=NAMES   only show expansions of the named macros`,
		Examples: []string{
			`nflow expand flows/client.nflow`,
			`nflow expand flows/client.nflow --macro=fetch_and_extract`,
			`nflow expand flows/client.nflow --macro=require_id,labeled`,
		},
	},
	"catalog": {
		Summary: "Dump the compiler surface (atoms, modifiers, directives, operators) as JSON.",
		Usage:   "nflow catalog [--flow FILE]",
		Flags: `      --flow FILE     load @require bundles from FILE before dumping
      --pretty        pretty-print JSON (default true)`,
		Examples: []string{
			`nflow catalog > catalog.json`,
			`nflow catalog --flow flows/client.nflow > catalog.json`,
		},
	},
	"self": {
		Summary: "Manage the installed nflow binary, run self-tests, or benchmark built-in scenarios.",
		Usage:   "nflow self <up|test|bench> [filter...] [flags]\n  nflow self bench [filter...] [--path FILE_OR_DIR] [--iterations N] [--warmup N] [--json] [--list]",
		Flags: `Self test:
      --verbose             print DSL on failure
      --json                machine-readable JSON summary
      --no-color            disable ANSI color
Self bench:
      --path FILE_OR_DIR    benchmark one file or discover a directory instead of built-ins
      --iterations N        measured iterations (default 50; range 1..1000000)
      --warmup N            unreported warmups (default 3; range 0..1000000)
      --json                machine-readable benchmark report
      --list                list matching scenarios without running them`,
		Examples: []string{
			`nflow self up                        # rebuild with VCS metadata from source tree`,
			`nflow self test                      # run the whole coverage suite`,
			`nflow self test operators            # only features in the Operators section`,
			`nflow self test loop assert          # anything matching "loop" OR "assert"`,
			`nflow self test §3.4                 # a single feature by ref`,
			`nflow self test --json > report.json # CI-friendly output`,
			`nflow self bench                    # benchmark built-in scenarios`,
			`nflow self bench api --iterations 100 --warmup 5`,
			`nflow self bench --path ./flows --list`,
		},
	},
	"require": {
		Summary: "Generate require glue or manage direct @require selectors in source.",
		Usage:   "nflow require <file.nflow> [-o out.go] [-pkg main] | nflow require <check|update|pin> <file.nflow> [--package <pattern>] | nflow require reset <file.nflow>",
		Flags:   `      --package PATTERN  select direct requirements by exact import or glob`,
		Examples: []string{
			`nflow require flows/client.nflow                 # generate require glue`,
			`nflow require check flows/client.nflow           # report canonical selectors and latest`,
			`nflow require update flows/client.nflow          # select latest for direct remotes`,
			`nflow require pin flows/client.nflow             # canonicalize existing selectors in source`,
			`nflow pin flows/client.nflow                     # alias for require pin`,
			`nflow require reset flows/client.nflow           # restore and consume .bak`,
		},
	},
	"pin": {
		Summary: "Alias for nflow require pin; canonicalizes direct remote selectors in source.",
		Usage:   "nflow pin <file.nflow> [--package <pattern>]",
		Flags:   `      --package PATTERN  select direct requirements by exact import or glob`,
		Examples: []string{
			`nflow pin flows/client.nflow`,
		},
	},
	"completion": {
		Summary: "Print a shell completion script.",
		Usage:   "nflow completion <bash|zsh|pwsh>",
		Examples: []string{
			`nflow completion bash > ~/.local/share/bash-completion/completions/nflow`,
			`nflow completion pwsh | Out-String | Invoke-Expression`,
		},
	},
	versionCommand: {
		Summary:  "Print build metadata offline, or check the latest stable GitHub release without updating.",
		Usage:    "nflow version [check]",
		Examples: []string{`nflow version`, `nflow version check`},
	},
}

func PrintTopHelp(w io.Writer) {
	fmt.Fprintln(w, "nflow — Nexss Flow compiler and runtime")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  nflow <command> [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")

	names := make([]string, 0, len(helpRegistry))
	for name := range helpRegistry {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(w, "  %-12s %s\n", name, helpRegistry[name].Summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `nflow help <command>` for usage and examples.")
}

func PrintCommandHelp(w io.Writer, command string) bool {
	text, ok := helpRegistry[command]
	if !ok {
		return false
	}

	fmt.Fprintf(w, "%s\n\n", text.Summary)
	fmt.Fprintf(w, "Usage:\n  %s\n\n", text.Usage)

	if text.Flags != "" {
		fmt.Fprintf(w, "Flags:\n%s\n\n", text.Flags)
	}

	if len(text.Examples) > 0 {
		fmt.Fprintln(w, "Examples:")
		for _, ex := range text.Examples {
			fmt.Fprintf(w, "  %s\n", ex)
		}
	}
	return true
}

func wantsHelp(args []string) bool {
	return len(args) > 0 && (args[0] == flagHelp || args[0] == flagHelpShort || args[0] == flagHelpWord)
}
