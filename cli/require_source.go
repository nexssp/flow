package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/builder"
)

const (
	requireCheck  = "check"
	requireUpdate = "update"
	requirePin    = "pin"
	requireReset  = "reset"
)

// runRequireSource is the sole frontend for source edits and the historical pin alias.
func runRequireSource(ctx context.Context, inv *invocation, operation string, args []string) int {
	usage := "nflow require " + operation + " <file.nflow>"
	if operation != requireReset {
		usage += " [--package <pattern>]"
	}
	file, pattern, help, err := parseRequireSourceArgs(args, operation != requireReset)
	if help {
		fmt.Fprintln(os.Stdout, "usage: "+usage)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage: "+usage)
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if operation == requireReset {
		if resetErr := builder.ResetRequirementSource(ctx, file); resetErr != nil {
			return fatalf("%v", resetErr)
		}
		fmt.Fprintf(os.Stdout, "Restored previous source stage: %s (backup consumed)\n", file)
		return 0
	}
	table, err := inv.directiveTable()
	if err != nil {
		return fatalf("directives: %v", err)
	}
	result, err := builder.ManageRequirementSource(ctx, file, operation, pattern, table)
	if err != nil {
		return fatalf("%v", err)
	}
	for _, change := range result.Changes {
		selector := change.Selector
		if selector == "" {
			selector = "(omitted)"
		}
		if change.Reason != "" {
			fmt.Fprintf(os.Stdout, "%s:%d: %s: skipped (%s)\n", file, change.Line, change.Import, change.Reason)
			continue
		}
		if operation == requireCheck {
			fmt.Fprintf(os.Stdout, "%s:%d: %s: selector %s; current %s; latest %s\n", file, change.Line, change.Import, selector, change.Current, change.Latest)
		} else {
			fmt.Fprintf(os.Stdout, "%s:%d: %s: %s -> %s\n", file, change.Line, change.Import, selector, change.Target)
		}
	}
	switch {
	case operation == requireCheck:
		if result.Changed {
			fmt.Fprintln(os.Stdout, "Source updates available; check made no changes.")
			return 1
		}
		fmt.Fprintln(os.Stdout, "No source updates available; check made no changes.")
	case result.Changed:
		fmt.Fprintf(os.Stdout, "Updated %s; previous source stage: %s\n", file, result.Backup)
	default:
		fmt.Fprintln(os.Stdout, "No source changes needed.")
	}
	return 0
}

// Accept flags on either side of the file, unlike flag.FlagSet's stop-at-file behavior.
func parseRequireSourceArgs(args []string, allowPackage bool) (file, pattern string, help bool, err error) {
	packageSeen, positionalOnly := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !positionalOnly {
			switch {
			case arg == "--":
				positionalOnly = true
				continue
			case arg == "--help" || arg == "-h":
				return "", "", true, nil
			case arg == "--package" || strings.HasPrefix(arg, "--package="):
				if !allowPackage {
					return "", "", false, errors.New("reset does not accept --package")
				}
				if packageSeen {
					return "", "", false, errors.New("--package may only be supplied once")
				}
				packageSeen = true
				if value, ok := strings.CutPrefix(arg, "--package="); ok {
					pattern = value
				} else {
					remaining := args[i+1:]
					if len(remaining) == 0 {
						return "", "", false, errors.New("--package requires a pattern")
					}
					pattern = remaining[0]
					i++
				}
				if pattern == "" {
					return "", "", false, errors.New("--package requires a non-empty pattern")
				}
				continue
			case strings.HasPrefix(arg, "-"):
				return "", "", false, fmt.Errorf("unknown option %s", arg)
			}
		}
		if file != "" {
			return "", "", false, errors.New("expected exactly one source file")
		}
		file = arg
	}
	if file == "" {
		return "", "", false, errors.New("a source file is required")
	}
	return file, pattern, false, nil
}
