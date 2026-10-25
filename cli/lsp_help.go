package cli

func init() {
	helpRegistry["lsp"] = helpText{
		Summary: "Start the Flow language server over stdio; external @require bundles need `nflow lint`.",
		Usage:   "nflow lsp",
		Examples: []string{
			`nflow lsp # start the compiler-backed editor language server`,
		},
	}
}
