package cli

// Well-known flag spellings shared across every subcommand.
//
// Every subcommand that accepts a help flag checks the same spellings.
// Centralizing them here means a spelling change lands in one place;
// before, the top-level dispatcher, the subcommand parsers, and
// wantsHelp each carried their own literal, and drift was possible.
const (
	flagHelp      = "--help"
	flagHelpShort = "-h"
	flagHelpWord  = "help"
)
