package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
)

func runShow(args []string) int {
	flowPath := ""
	for i := range args {
		switch {
		case args[i] == "--flow" && i+1 < len(args):
			flowPath = args[i+1]
		case strings.HasPrefix(args[i], "--flow="):
			flowPath = strings.TrimPrefix(args[i], "--flow=")
		}
	}
	return withHarness("show", flowPath, args, runShowInProcess)
}

func runShowInProcess(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	flowPath := fs.String("flow", "", "load @require bundles from this .nflow first")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target := strings.TrimSpace(fs.Arg(0))
	if target == "" {
		return fatalf("usage: nflow show <name>")
	}

	loaded, err := sourceRequiresFromOptional(*flowPath)
	if err != nil {
		return fatalf("%v", err)
	}

	cfg, err := buildConfig(loaded)
	if err != nil {
		return fatalf("config: %v", err)
	}

	cat := core.BuildCatalog(cfg.Resolver, cfg.Modifiers, cfg.Directives, cfg.Operators)
	useColor := !*noColor && colorEnabled(os.Stdout)

	switch {
	case showAtom(cat.Atoms, target, useColor):
		return 0
	case showModifier(cat.Modifiers, target, useColor):
		return 0
	case showDirective(cat.Directives, target, useColor):
		return 0
	case showOperator(cat.Operators, target, useColor):
		return 0
	default:
		return fatalf("no atom, modifier, directive or operator named %q", target)
	}
}

func sourceRequiresFromOptional(path string) ([]require.Requirement, error) {
	if path == "" {
		return nil, nil
	}
	return sourceRequiresFromFile(path)
}

func showAtom(atoms []core.AtomSpec, name string, useColor bool) bool {
	for i := range atoms {
		a := &atoms[i]
		if a.Name != name {
			continue
		}

		line := "  " + cyan(a.Name, useColor)
		pad := max(60-len(a.Name), 1)
		line += strings.Repeat(" ", pad) + dim("atom", useColor)
		fmt.Fprintln(os.Stdout, line)
		fmt.Fprintln(os.Stdout, "  "+strings.Repeat("─", 70))
		if a.Description != "" {
			fmt.Fprintf(os.Stdout, "  %s\n", a.Description)
		}
		fmt.Fprintln(os.Stdout)

		if len(a.Tags) > 0 {
			fmt.Fprintf(os.Stdout, "  %-10s %s\n", dim("Tags", useColor), strings.Join(a.Tags, ", "))
		}
		if a.Scope != "" {
			fmt.Fprintf(os.Stdout, "  %-10s %s\n", dim("Scope", useColor), a.Scope)
		}

		renderFieldSection("Input", a.ReqFields, useColor)
		renderFieldSection("Output", a.ResFields, useColor)

		if len(a.Example) > 0 {
			fmt.Fprintf(os.Stdout, "\n  %s\n", bold("EXAMPLE", useColor))
			var buf any
			if err := json.Unmarshal(a.Example, &buf); err == nil {
				if pretty, err := json.MarshalIndent(buf, "    ", "  "); err == nil {
					fmt.Fprintf(os.Stdout, "    %s\n", string(pretty))
				} else {
					fmt.Fprintf(os.Stdout, "    %s\n", string(a.Example))
				}
			} else {
				fmt.Fprintf(os.Stdout, "    %s\n", string(a.Example))
			}
		}
		fmt.Fprintln(os.Stdout)
		return true
	}
	return false
}

func renderFieldSection(title string, fields []core.FieldSpec, useColor bool) {
	if len(fields) == 0 {
		return
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	fmt.Fprintf(os.Stdout, "\n  %s\n", bold(title, useColor))
	for _, f := range fields {
		req := ""
		if f.Required {
			req = " " + red("required", useColor)
		}
		usage := ""
		if f.Usage != "" {
			usage = "  " + dim(f.Usage, useColor)
		}
		fmt.Fprintf(os.Stdout, "    %-16s %-24s%s%s\n", f.Name, f.Type, req, usage)
	}
}

func showModifier(mods []core.ModifierSpec, name string, useColor bool) bool {
	for _, m := range mods {
		if m.Name != name {
			continue
		}
		fmt.Fprintf(os.Stdout, "\n  %s\n  %s\n\n", cyan(":"+m.Name, useColor), strings.Repeat("─", 70))
		if m.Example != "" {
			fmt.Fprintf(os.Stdout, "  Example: %s\n", m.Example)
		}
		fmt.Fprintln(os.Stdout)
		return true
	}
	return false
}

func showDirective(dirs []core.DirectiveSpec, name string, useColor bool) bool {
	for _, d := range dirs {
		if d.Name != name {
			continue
		}
		fmt.Fprintf(os.Stdout, "\n  %s\n  %s\n\n", cyan("@"+d.Name, useColor), strings.Repeat("─", 70))

		if d.Example != "" {
			fmt.Fprintf(os.Stdout, "  Example:\n    %s\n", strings.ReplaceAll(d.Example, "\n", "\n    "))
		}
		fmt.Fprintln(os.Stdout)
		return true
	}
	return false
}

func showOperator(ops []core.OperatorSpec, name string, useColor bool) bool {
	for _, o := range ops {
		if o.Name != name {
			continue
		}
		fmt.Fprintf(os.Stdout, "\n  %s  %s\n  %s\n\n",
			cyan(o.Name, useColor),
			dim("("+o.Token+")", useColor),
			strings.Repeat("─", 70))
		if o.Description != "" {
			fmt.Fprintf(os.Stdout, "  %s\n", o.Description)
		}

		if o.Example != "" {
			fmt.Fprintf(os.Stdout, "  Example: %s\n", o.Example)
		}
		fmt.Fprintln(os.Stdout)
		return true
	}
	return false
}
