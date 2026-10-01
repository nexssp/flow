package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/runner"
)

// LintIssue is one problem found in a .nflow source. The shape is
// stable — the CLI prints it as JSON so tooling and editors can
// consume it. File is always a real path (never a synthetic label).
type LintIssue struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func runLint(args []string) int {
	path := ""
	if len(args) > 0 {
		path = args[0]
	}
	return withHarness("lint", path, args, runLintInProcess)
}

func runLintInProcess(args []string) int {
	if len(args) < 1 {
		return fatalf("usage: nflow lint <file.nflow>")
	}
	path := args[0]

	src, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read: %v", err)
	}

	reqs, err := sourceRequiresFromFile(path)
	if err != nil {
		return fatalf("requires: %v", err)
	}

	cfg, err := buildConfig(reqs)
	if err != nil {
		return fatalf("config: %v", err)
	}

	atoms, modifiers := registrySurface(cfg)
	issues := lintFile(path, string(src), cfg, atoms, modifiers)

	if len(issues) == 0 {
		fmt.Fprintln(os.Stdout, "ok")
		return 0
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(issues); err != nil {
		return fatalf("encode issues: %v", err)
	}
	return 1
}

// registrySurface snapshots the names the compiler knows about. Four
// categories share one flat namespace at parse time: atoms, sources,
// operators, and boundaries. Modifiers are validated only for atoms —
// sources, operators, and boundaries either take no modifiers or
// receive them as config through ModifiersToMap, not as modifiers to
// the builder.
func registrySurface(cfg runner.Config) (known, modifiers map[string]struct{}) {
	cat := core.BuildCatalog(cfg.Resolver, cfg.Modifiers, cfg.Directives, cfg.Operators)

	known = make(map[string]struct{},
		len(cat.Atoms)+len(cat.Sources)+len(cat.Operators))

	for i := range cat.Atoms {
		known[cat.Atoms[i].Name] = struct{}{}
	}
	for _, s := range cat.Sources {
		known[s.Name] = struct{}{}
	}
	for _, o := range cat.Operators {
		known[o.Name] = struct{}{}
	}
	for _, b := range core.NamedBoundaries() {
		known[b.Name] = struct{}{}
	}

	modifiers = make(map[string]struct{}, len(cat.Modifiers))
	for _, m := range cat.Modifiers {
		modifiers[m.Name] = struct{}{}
	}
	return known, modifiers
}

// lintFile processes one source: preprocess, lint the top-level
// pipeline if present, then lint every @pipeline fragment in
// isolation. Library files (only @pipeline blocks, no top-level) are
// valid — the emptiness of the top-level body is not an error.
func lintFile(path, src string, cfg runner.Config, atoms, modifiers map[string]struct{}) []LintIssue {
	clean, meta, err := core.Preprocess(context.Background(), cfg.Directives, src, path)
	if err != nil {
		return []LintIssue{{
			File:    path,
			Kind:    "preprocess",
			Message: err.Error(),
		}}
	}

	known := make(map[string]struct{}, len(atoms))
	for name := range atoms {
		known[name] = struct{}{}
	}

	pipelines, _ := meta["pipelines"].(map[string]string)
	for name := range pipelines {
		known[name] = struct{}{}
	}

	var issues []LintIssue

	if strings.TrimSpace(clean) != "" {
		issues = append(issues, lintFragment(path, clean, 0, cfg, known, modifiers)...)
	}

	for name, body := range pipelines {
		fragmentClean, _, perr := core.Preprocess(context.Background(), cfg.Directives, body, name)
		if perr != nil {
			issues = append(issues, LintIssue{
				File:    path,
				Kind:    "pipeline_preprocess",
				Message: fmt.Sprintf("@pipeline %s: %v", name, perr),
			})
			continue
		}
		if strings.TrimSpace(fragmentClean) == "" {
			continue
		}

		label := fmt.Sprintf("%s:@pipeline[%s]", path, name)
		issues = append(issues, lintFragment(label, fragmentClean, 0, cfg, known, modifiers)...)
	}

	return issues
}

func lintFragment(path, src string, lineBase int, cfg runner.Config, known, modifiers map[string]struct{}) []LintIssue {
	ast, err := core.NewParserWithFileOffset(
		context.Background(),
		cfg.Operators,
		cfg.Primaries,
		src,
		path,
		lineBase,
	).Parse()
	if err != nil {
		return []LintIssue{{
			File:    path,
			Kind:    "parse",
			Message: err.Error(),
		}}
	}

	var issues []LintIssue
	walkLintAST(ast, func(atom *core.Atom) {
		_, isKnown := known[atom.Name]
		if !isKnown {
			issues = append(issues, LintIssue{
				File:    path,
				Kind:    "unknown_atom",
				Message: fmt.Sprintf("%q is not a registered atom, source, operator, or boundary", atom.Name),
			})
			return
		}

		// Modifier validation is meaningful only for atoms that route
		// through ModifierTable.ApplyAll. Sources, operators, and
		// boundaries do not: sources and operators read :key=value
		// through ModifiersToMap in ast_build, and boundaries take no
		// modifiers at all.
		if isNonAtomCapability(cfg, atom.Name) {
			return
		}

		action, _ := cfg.Resolver.Action(atom.Name)

		for _, raw := range atom.Modifiers {
			name := raw
			if i := strings.IndexByte(name, '='); i > 0 {
				name = name[:i]
			}
			if _, ok := modifiers[name]; ok {
				continue
			}

			message := fmt.Sprintf("modifier :%s is not registered", name)
			if action != nil {
				if hint := core.SuggestModifierFix(action, name); hint != "" {
					message = hint
				}
			}

			issues = append(issues, LintIssue{
				File:    path,
				Kind:    "unknown_modifier",
				Message: message,
			})
		}
	})

	return issues
}

// isNonAtomCapability reports whether the name resolves through one of
// the non-action tables: streams, operators, or boundaries. Modifiers
// on such names are config values, not builder modifiers, so the lint
// pass skips modifier validation for them.
func isNonAtomCapability(cfg runner.Config, name string) bool {
	if _, ok := cfg.Resolver.Stream(name); ok {
		return true
	}
	if _, ok := cfg.Resolver.Operator(name); ok {
		return true
	}
	if _, ok := core.BoundaryByName(name); ok {
		return true
	}
	return false
}

func walkLintAST(node core.Expr, visit func(*core.Atom)) {
	switch n := node.(type) {
	case *core.Atom:
		visit(n)
	case *core.PipeExpr:
		walkLintAST(n.L, visit)
		walkLintAST(n.R, visit)
	case *core.ParallelExpr:
		for _, c := range n.Branches {
			walkLintAST(c, visit)
		}
	case *core.FallbackExpr:
		walkLintAST(n.L, visit)
		walkLintAST(n.R, visit)
	case *core.ConditionalExpr:
		walkLintAST(n.Cond, visit)
		walkLintAST(n.Then, visit)
		if n.Else != nil {
			walkLintAST(n.Else, visit)
		}
	case *core.LoopExpr:
		walkLintAST(n.Body, visit)
	case *core.AssertExpr, *core.ProjectionExpr:
		// Assert and projection carry raw expressions, not atom
		// references. Nothing to validate at this layer.
	}
}
