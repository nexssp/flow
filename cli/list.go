package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/core"
)

func runList(args []string) int {
	flowPath := ""
	var cleanArgs []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--flow" && i+1 < len(args):
			flowPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--flow="):
			flowPath = strings.TrimPrefix(args[i], "--flow=")
		case strings.HasSuffix(args[i], ".nflow"):
			if flowPath == "" {
				flowPath = args[i]
			} else {
				cleanArgs = append(cleanArgs, args[i])
			}
		default:
			cleanArgs = append(cleanArgs, args[i])
		}
	}
	return withHarness("list", flowPath, cleanArgs, runListInProcess)
}

func runListInProcess(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	flowPath := fs.String("flow", "", "load @require bundles from this .nflow first")
	jsonOut := fs.Bool("json", false, "output as JSON instead of a rendered list")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")

	if err := fs.Parse(args); err != nil {
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

	pattern := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	if strings.HasSuffix(pattern, ".nflow") {
		pattern = "" // Do not search for the filename as a pattern
	}

	var cfg core.Catalog
	if *flowPath != "" {
		loaded, err := sourceRequiresFromFile(*flowPath)
		if err != nil {
			return fatalf("read %s: %v", *flowPath, err)
		}
		runnerCfg, err := buildConfig(loaded)
		if err != nil {
			return fatalf("config: %v", err)
		}
		cfg = core.BuildCatalog(runnerCfg.Resolver, runnerCfg.Modifiers, runnerCfg.Directives, runnerCfg.Operators)
	} else {
		runnerCfg, err := buildConfig(nil)
		if err != nil {
			return fatalf("config: %v", err)
		}
		cfg = core.BuildCatalog(runnerCfg.Resolver, runnerCfg.Modifiers, runnerCfg.Directives, runnerCfg.Operators)
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

func renderList(cat core.Catalog, pattern string, useColor bool) {
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

func atomMatches(a core.AtomSpec, pattern string) bool {
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

func matchesAtoms(atoms []core.AtomSpec, pattern string) bool {
	for i := range atoms {
		if atomMatches(atoms[i], pattern) {
			return true
		}
	}
	return false
}

func matchesSimpleMods(mods []core.ModifierSpec, pattern string) bool {
	for _, m := range mods {
		if strings.Contains(strings.ToLower(m.Name), pattern) {
			return true
		}
	}
	return false
}

func matchesSimpleDirs(dirs []core.DirectiveSpec, pattern string) bool {
	for _, d := range dirs {
		if strings.Contains(strings.ToLower(d.Name), pattern) {
			return true
		}
	}
	return false
}

func matchesSimpleOps(ops []core.OperatorSpec, pattern string) bool {
	for _, o := range ops {
		if strings.Contains(strings.ToLower(o.Name), pattern) {
			return true
		}
	}
	return false
}

func sourceMatches(s core.SourceSpec, pattern string) bool {
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

func matchesSources(sources []core.SourceSpec, pattern string) bool {
	for _, s := range sources {
		if sourceMatches(s, pattern) {
			return true
		}
	}
	return false
}
