package cli

import (
	"flag"
	"os"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
)

func runCatalog(args []string) int {
	flowPath := extractCatalogFlowPath(args)
	return withHarness("catalog", flowPath, args, runCatalogInProcess)
}

func extractCatalogFlowPath(args []string) string {
	for i := range args {
		switch {
		case args[i] == "--flow" && i+1 < len(args):
			return args[i+1]
		case len(args[i]) > 7 && args[i][:7] == "--flow=":
			return args[i][7:]
		}
	}
	return ""
}

func runCatalogInProcess(args []string) int {
	fs := flag.NewFlagSet("catalog", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	pretty := fs.Bool("pretty", true, "pretty-print JSON")
	flowPath := fs.String("flow", "", "load @require bundles from this .nflow first")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	var reqs []require.Requirement
	if *flowPath != "" {
		loaded, err := sourceRequiresFromFile(*flowPath)
		if err != nil {
			return fatalf("read %s: %v", *flowPath, err)
		}
		reqs = loaded
	}

	cfg, err := buildConfig(reqs)
	if err != nil {
		return fatalf("config: %v", err)
	}

	cat := core.BuildCatalog(cfg.Resolver, cfg.Modifiers, cfg.Directives, cfg.Operators)

	if err := writeJSON(os.Stdout, cat, *pretty); err != nil {
		return fatalf("encode: %v", err)
	}
	return 0
}
