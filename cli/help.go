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
  --info              describe the pipeline instead of executing`,
		Examples: []string{
			`nflow run flows/client.nflow '{"user_id": 42}'`,
			`nflow run flows/client.nflow --assert='result.ok == true'`,
			`nflow run -vv flows/client.nflow`,
			`nflow run --info flows/client.nflow`,
		},
	},
	"build": {
		Summary: "Compile a .nflow file into a standalone executable with embedded source and @require modules.",
		Usage:   "nflow build <file.nflow> -o <output> [flags]",
		Flags:   `  -o FILE             output binary (required)`,
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
		Summary: "Check .nflow sources against the live registry. Exits 1 and prints JSON issues on failure.",
		Usage:   "nflow lint <file.nflow | directory | ./...> [target...]",
		Examples: []string{
			`nflow lint flows/client.nflow`,
			`nflow lint ./...`,
			`nflow lint ./examples/...`,
		},
	},
	"explain": {
		Summary: "Show the effective modifiers for each atom, with source attribution. With --ast, render the effective AST after every directive and macro has been resolved.",
		Usage:   "nflow explain <file.nflow> [--ast] [--json]",
		Flags: `      --ast         show the effective AST instead of the policy view
      --json        emit the AST as JSON (requires --ast)`,
		Examples: []string{
			`nflow explain flows/client.nflow`,
			`nflow explain --ast flows/client.nflow`,
			`nflow explain --ast --json flows/client.nflow > ast.json`,
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
		Summary: "Manage the installed nflow binary, or run the embedded feature coverage suite.",
		Usage:   "nflow self <up|test> [filter...]",
		Flags: `      --verbose     print DSL on failure
      --json        machine-readable JSON summary
      --no-color    disable ANSI color`,
		Examples: []string{
			`nflow self up                        # rebuild with VCS metadata from source tree`,
			`nflow self test                      # run the whole coverage suite`,
			`nflow self test operators            # only features in the Operators section`,
			`nflow self test loop assert          # anything matching "loop" OR "assert"`,
			`nflow self test §3.4                 # a single feature by ref`,
			`nflow self test --json > report.json # CI-friendly output`,
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
	"version": {
		Summary:  "Print build metadata (version, commit, built at).",
		Usage:    "nflow version",
		Examples: []string{`nflow version`},
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
