package cli

func init() {
	helpRegistry["fmt"] = helpText{
		Summary: "Format .nflow source without running it. Preprocessing discovers required grammar; use `nflow lint` for full compiler validation, including unlinked @require bundles.",
		Usage:   "nflow fmt [--write | --check] [--stdin] [FILE ...]",
		Flags: `      --write   atomically rewrite named files
      --check   exit non-zero if named files need formatting
      --stdin   read source from stdin and write formatted source to stdout
      -         read stdin when used as the single input`,
		Examples: []string{
			`nflow fmt flows/client.nflow > formatted.nflow`,
			`nflow fmt --write flows/client.nflow`,
			`nflow fmt --check flows/*.nflow`,
			`printf 'source->action\n' | nflow fmt --stdin`,
		},
	}
}
