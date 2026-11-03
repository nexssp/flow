package cli

func init() {
	helpRegistry["lsp"] = helpText{
		Summary: "Start the Flow language server over stdio; workspace @require bundles are resolved and cached automatically.",
		Usage:   "nflow lsp",
		Examples: []string{
			`nflow lsp # start the compiler-backed editor language server`,
		},
	}
}
