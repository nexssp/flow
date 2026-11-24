package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/compiler"
)

const (
	flowPathFlag       = "--flow"
	flowPathFlagPrefix = flowPathFlag + "="
)

func runList(ctx context.Context, inv *invocation, args []string) int {
	return withHarness(ctx, inv, "list", listFlowPath(args), args, runListInProcess)
}

func listFlowPath(args []string) string {
	flowPath := ""
	positionalPath := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == flowPathFlag && i+1 < len(args):
			flowPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], flowPathFlagPrefix):
			flowPath = strings.TrimPrefix(args[i], flowPathFlagPrefix)
		case args[i] == "--":
			for _, arg := range args[i+1:] {
				if positionalPath == "" && strings.HasSuffix(arg, ".nflow") {
					positionalPath = arg
				}
			}
			return firstNonEmpty(flowPath, positionalPath)
		case strings.HasSuffix(args[i], ".nflow") && positionalPath == "":
			positionalPath = args[i]
		}
	}
	return firstNonEmpty(flowPath, positionalPath)
}

func optionsBeforePositionals(args []string) []string {
	options := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			options = append(options, arg)
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if arg == flowPathFlag && i+1 < len(args) {
			options = append(options, arg, args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			options = append(options, arg)
		} else {
			positionals = append(positionals, arg)
		}
	}
	return append(options, positionals...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func runListInProcess(ctx context.Context, inv *invocation, args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	flowPath := fs.String("flow", "", "load @require bundles from this .nflow first")
	jsonOut := fs.Bool("json", false, "output as JSON instead of a rendered list")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")

	if err := fs.Parse(optionsBeforePositionals(args)); err != nil {
		return 2
	}

	// Support positional file argument
	if *flowPath == "" {
		for _, arg := range fs.Args() {
			if strings.HasSuffix(arg, ".nflow") {
				*flowPath = arg
				break
			}
		}
	}

	pattern := ""
	for _, arg := range fs.Args() {
		if !strings.HasSuffix(arg, ".nflow") {
			pattern = strings.ToLower(strings.TrimSpace(arg))
			break
		}
	}

	var cfg compiler.Catalog
	if *flowPath != "" {
		loaded, err := inv.sourceRequiresFromFile(ctx, *flowPath)
		if err != nil {
			return fatalf("read %s: %v", *flowPath, err)
		}
		runnerCfg, err := inv.buildConfig(loaded)
		if err != nil {
			return fatalf("config: %v", err)
		}
		cfg = compiler.BuildCatalog(runnerCfg.Resolver, runnerCfg.Modifiers, runnerCfg.Directives, runnerCfg.Operators)
	} else {
		runnerCfg, err := inv.buildConfig(nil)
		if err != nil {
			return fatalf("config: %v", err)
		}
		cfg = compiler.BuildCatalog(runnerCfg.Resolver, runnerCfg.Modifiers, runnerCfg.Directives, runnerCfg.Operators)
	}

	if *jsonOut {
		if err := writeJSON(os.Stdout, cfg, true); err != nil {
			return fatalf("encode: %v", err)
		}
		return 0
	}

	useColor := !*noColor && colorEnabled(os.Stdout)
	renderList(cfg, pattern, useColor)
	return 0
}

func renderList(cat compiler.Catalog, pattern string, useColor bool) {
	if matchesSources(cat.Sources, pattern) {
		fmt.Fprintf(os.Stdout, "\n%s\n\n", bold("SOURCES", useColor))
		for _, s := range cat.Sources {
			if !sourceMatches(s, pattern) {
				continue
			}
			fmt.Fprintf(os.Stdout, "  %-32s %s  %s\n",
				cyan(s.Name, useColor),
				dim(padRight("source", 8), useColor),
				s.Description,
			)
		}
	}

	if matchesAtoms(cat.Atoms, pattern) {
		fmt.Fprintf(os.Stdout, "\n%s\n\n", bold("ATOMS", useColor))
		for i := range cat.Atoms {
			a := cat.Atoms[i]
			if !atomMatches(a, pattern) {
				continue
			}
			fmt.Fprintf(os.Stdout, "  %-32s %s  %s\n",
				cyan(a.Name, useColor),
				dim(padRight("atom", 8), useColor),
				a.Description,
			)
		}
	}

	if matchesSimpleMods(cat.Modifiers, pattern) {
		fmt.Fprintf(os.Stdout, "\n%s\n\n", bold("MODIFIERS", useColor))
		for _, m := range cat.Modifiers {
			if !strings.Contains(strings.ToLower(m.Name), pattern) {
				continue
			}
			fmt.Fprintf(os.Stdout, "  %s\n", cyan(":"+m.Name, useColor))
		}
	}

	if matchesSimpleDirs(cat.Directives, pattern) {
		fmt.Fprintf(os.Stdout, "\n%s\n\n", bold("DIRECTIVES", useColor))
		for _, d := range cat.Directives {
			if !strings.Contains(strings.ToLower(d.Name), pattern) {
				continue
			}
			ref := ""

			fmt.Fprintf(os.Stdout, "  %s%s\n", cyan("@"+d.Name, useColor), ref)
		}
	}

	if matchesSimpleOps(cat.Operators, pattern) {
		fmt.Fprintf(os.Stdout, "\n%s\n\n", bold("OPERATORS", useColor))
		for _, o := range cat.Operators {
			if !strings.Contains(strings.ToLower(o.Name), pattern) {
				continue
			}
			fmt.Fprintf(os.Stdout, "  %-16s %-8s %s\n",
				cyan(o.Name, useColor),
				dim(o.Token, useColor),
				o.Description,
			)
		}
	}

	fmt.Fprintln(os.Stdout)
}

func atomMatches(a compiler.AtomSpec, pattern string) bool {
	if pattern == "" {
		return true
	}
	if strings.Contains(strings.ToLower(a.Name), pattern) ||
		strings.Contains(strings.ToLower(a.Description), pattern) {
		return true
	}
	for _, t := range a.Tags {
		if strings.Contains(strings.ToLower(t), pattern) {
			return true
		}
	}
	return false
}

func matchesAtoms(atoms []compiler.AtomSpec, pattern string) bool {
	for i := range atoms {
		if atomMatches(atoms[i], pattern) {
			return true
		}
	}
	return false
}

func matchesSimpleMods(mods []compiler.ModifierSpec, pattern string) bool {
	for _, m := range mods {
		if strings.Contains(strings.ToLower(m.Name), pattern) {
			return true
		}
	}
	return false
}

func matchesSimpleDirs(dirs []compiler.DirectiveSpec, pattern string) bool {
	for _, d := range dirs {
		if strings.Contains(strings.ToLower(d.Name), pattern) {
			return true
		}
	}
	return false
}

func matchesSimpleOps(ops []compiler.OperatorSpec, pattern string) bool {
	for _, o := range ops {
		if strings.Contains(strings.ToLower(o.Name), pattern) {
			return true
		}
	}
	return false
}

func sourceMatches(s compiler.SourceSpec, pattern string) bool {
	if pattern == "" {
		return true
	}
	if strings.Contains(strings.ToLower(s.Name), pattern) ||
		strings.Contains(strings.ToLower(s.Description), pattern) {
		return true
	}
	for _, t := range s.Tags {
		if strings.Contains(strings.ToLower(t), pattern) {
			return true
		}
	}
	return false
}

func matchesSources(sources []compiler.SourceSpec, pattern string) bool {
	for _, s := range sources {
		if sourceMatches(s, pattern) {
			return true
		}
	}
	return false
}
