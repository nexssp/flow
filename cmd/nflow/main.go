package main

import (
	"os"

	"github.com/nexssp/flow/cli"
)

// Bundle registration for the nflow binary itself lives in
// cmd/nflow/imports.go. This file only wires the entry point.
func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
