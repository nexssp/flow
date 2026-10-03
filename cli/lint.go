package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
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
	if len(args) == 0 {
		return fatalf("usage: nflow lint <file.nflow | directory | ./...>")
	}

	// Preserve the existing explicit single-file path, including its
	// source-specific harness and error behavior.
	if len(args) == 1 && !isRecursiveLintPattern(args[0]) {
		info, err := os.Stat(args[0])
		if err != nil || !info.IsDir() {
			return withHarness("lint", args[0], args, runLintInProcess)
		}
	}

	files, err := discoverLintFiles(args)
	if err != nil {
		return fatalf("%v", err)
	}

	// Make every required bundle available to an optional external harness,
	// but build each file's actual registry independently in batch linting.
	var allRequirements []require.Requirement
	for _, path := range files {
		reqs, reqErr := sourceRequiresFromFile(path)
		if reqErr == nil {
			allRequirements = append(allRequirements, reqs...)
		}
	}
	if os.Getenv(harnessEnv) != "" || !requirementsNeedExternalHarness(allRequirements) {
		return runLintBatchInProcess(files)
	}
	bin, err := EnsureHarness(allRequirements, files[0])
	if err != nil {
		return runLintBatchInProcessWithHarnessError(files, err)
	}
	return ExecHarness(bin, append([]string{"lint"}, files...))
}

var lintDiscoveryExcludedDirs = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	"vendor":       {},
	"node_modules": {},
}

func isRecursiveLintPattern(target string) bool {
	return target == "..." || strings.HasSuffix(target, "/...") ||
		(filepath.Separator == '\\' && strings.HasSuffix(target, `\...`))
}

// discoverLintFiles expands exact directories and Go-style /... targets.
// WalkDir does not follow directory symlinks; discovered symlink files are
// also skipped so a link cannot make a source appear more than once.
func discoverLintFiles(targets []string) ([]string, error) {
	seen := make(map[string]struct{})
	files := make([]string, 0)
	add := func(path string) {
		clean := filepath.Clean(path)
		identity, err := filepath.Abs(clean)
		if err != nil {
			identity = clean
		}
		if _, ok := seen[identity]; ok {
			return
		}
		seen[identity] = struct{}{}
		files = append(files, clean)
	}

	for _, target := range targets {
		if isRecursiveLintPattern(target) {
			root := strings.TrimSuffix(target, "...")
			root = strings.TrimRight(root, `/\`)
			if root == "" {
				root = "."
			}
			info, err := os.Stat(root)
			if err != nil {
				return nil, fmt.Errorf("lint: target %q: %w", target, err)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("lint: target %q is not a directory", target)
			}
			found, err := walkLintSources(root, add)
			if err != nil {
				return nil, fmt.Errorf("lint: walk %q: %w", root, err)
			}
			if found == 0 {
				return nil, fmt.Errorf("lint: no .nflow files found for target %q", target)
			}
			continue
		}

		info, err := os.Stat(target)
		if err != nil {
			return nil, fmt.Errorf("lint: target %q: %w", target, err)
		}
		if !info.IsDir() {
			add(target)
			continue
		}
		found, err := walkLintSources(target, add)
		if err != nil {
			return nil, fmt.Errorf("lint: walk %q: %w", target, err)
		}
		if found == 0 {
			return nil, fmt.Errorf("lint: no .nflow files found in directory %q", target)
		}
	}

	sort.Strings(files)
	return files, nil
}

func walkLintSources(root string, add func(string)) (int, error) {
	cleanRoot := filepath.Clean(root)
	if cleanRoot != "." {
		if _, excluded := lintDiscoveryExcludedDirs[filepath.Base(cleanRoot)]; excluded {
			return 0, nil
		}
	}
	found := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root {
				if _, skip := lintDiscoveryExcludedDirs[entry.Name()]; skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".nflow") {
			return nil
		}
		found++
		add(path)
		return nil
	})
	return found, err
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

func runLintBatchInProcess(paths []string) int {
	return runLintBatchInProcessWithHarnessError(paths, nil)
}

func runLintBatchInProcessWithHarnessError(paths []string, harnessErr error) int {
	var issues []LintIssue
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, LintIssue{File: path, Kind: "read", Message: err.Error()})
			continue
		}

		reqs, reqErr := sourceRequiresFromFile(path)
		if reqErr != nil {
			// lintFile reports preprocessing failures as source diagnostics;
			// do not turn one bad source into an early batch abort.
			reqs = nil
		}
		if harnessErr != nil && requirementsNeedExternalHarness(reqs) {
			issues = append(issues, LintIssue{
				File:    path,
				Kind:    "config",
				Message: fmt.Sprintf("external harness unavailable: %v", harnessErr),
			})
			continue
		}
		cfg, err := buildConfig(reqs)
		if err != nil {
			issues = append(issues, LintIssue{File: path, Kind: "config", Message: err.Error()})
			continue
		}
		atoms, modifiers := registrySurface(cfg)
		issues = append(issues, lintFile(path, string(src), cfg, atoms, modifiers)...)
	}

	if len(issues) == 0 {
		fmt.Fprintf(os.Stdout, "ok (%d files)\n", len(paths))
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
// operators, and boundaries. Modifiers are returned as full values so
// the linter can validate their declared value kinds against the DSL
// text, not just check that the name exists.
func registrySurface(cfg runner.Config) (known map[string]struct{}, modifiers map[string]core.Modifier) {
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

	modifiers = make(map[string]core.Modifier, len(cat.Modifiers))
	if cfg.Modifiers != nil {
		for _, m := range cfg.Modifiers.All() {
			modifiers[m.Name] = m
		}
	}
	return known, modifiers
}

// computeLineMods returns the line-indexed modifier lookups a source
// with the given meta would install. It is the lint-side counterpart
// of the pipeline applied inside CompileAction: read Preprocess
// contributions, extract the line lookups, filter through the modifier
// table.
func computeLineMods(cfg runner.Config, meta map[string]any) []core.LineLookup {
	opts := append([]core.CompileOption(nil), cfg.CompileOpts...)
	contribs := core.PreprocessContributionsFromMeta(meta, cfg.CompileOpts...)
	opts = append(opts, contribs.CompileOpts...)
	return core.LineModifiersFromOptions(cfg.Modifiers, opts)
}

// lintFile processes one source: preprocess, lint the top-level
// pipeline if present, then lint every @pipeline fragment in
// isolation. Library files (only @pipeline blocks, no top-level) are
// valid — the emptiness of the top-level body is not an error.
//
// Each fragment is parsed with the line-indexed modifier lookups the
// runtime compiler would apply: @scope spans from the fragment body,
// @pipeline :profile-derived policy modifiers, and top-level @scope
// spans that reach into the fragment. This makes lint agree with the
// compiler on inherited modifiers, so a typo inside @scope or
// @profile fails at lint time, not just at run time.
func lintFile(
	path, src string,
	cfg runner.Config,
	atoms map[string]struct{},
	modifiers map[string]core.Modifier,
) []LintIssue {
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
		if !strings.Contains(name, ".") {
			known["pipeline."+name] = struct{}{}
		}
	}

	topLineMods := computeLineMods(cfg, meta)

	var issues []LintIssue

	if strings.TrimSpace(clean) != "" {
		issues = append(issues, lintFragment(path, clean, cfg, known, modifiers, topLineMods)...)
	}

	pipelineMods, _ := meta["pipeline_modifiers"].(map[string][]string)

	pipelineNames := make([]string, 0, len(pipelines))
	for name := range pipelines {
		pipelineNames = append(pipelineNames, name)
	}
	sort.Strings(pipelineNames)
	for _, name := range pipelineNames {
		body := pipelines[name]
		fragmentClean, fragmentMeta, perr := core.Preprocess(context.Background(), cfg.Directives, body, name)
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

		fragmentOpts := append([]core.CompileOption(nil), cfg.CompileOpts...)
		fragmentContribs := core.PreprocessContributionsFromMeta(fragmentMeta, cfg.CompileOpts...)
		fragmentOpts = append(fragmentOpts, fragmentContribs.CompileOpts...)

		if mods, ok := pipelineMods[name]; ok && len(mods) > 0 {
			_, bodyMods := runner.SplitPipelineModifiers(cfg.Modifiers, mods)
			if len(bodyMods) > 0 {
				captured := append([]string(nil), bodyMods...)
				fragmentOpts = append(fragmentOpts, core.WithLineModifiers(core.LineLookup{
					Source: core.ModifierSource{
						Kind:  "pipeline",
						Label: name,
					},
					Fn: func(int) []string {
						return captured
					},
				}))
			}
		}

		fragmentLineMods := core.LineModifiersFromOptions(cfg.Modifiers, fragmentOpts)
		label := fmt.Sprintf("%s:@pipeline[%s]", path, name)
		issues = append(issues, lintFragment(label, fragmentClean, cfg, known, modifiers, fragmentLineMods)...)
	}

	return issues
}

// lintFragment parses one source fragment with the given line-indexed
// modifier lookups installed. The parser consults the lookups for
// every atom, so inherited modifiers are validated exactly as the
// runtime compiler would.
func lintFragment(
	path, src string,
	cfg runner.Config,
	known map[string]struct{},
	modifiers map[string]core.Modifier,
	lineMods []core.LineLookup,
) []LintIssue {
	ast, err := core.NewParserWithFileOffset(
		context.Background(),
		cfg.Operators,
		cfg.Primaries,
		src,
		path,
		0,
	).
		WithLineModifiers(lineMods...).
		Parse()
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
			name, value := splitRawModifier(raw)

			modifier, ok := modifiers[name]
			if !ok {
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
				continue
			}

			if err := core.ValidateModifierValue(modifier.ValueKind, value); err != nil {
				issues = append(issues, LintIssue{
					File:    path,
					Kind:    "invalid_modifier_value",
					Message: fmt.Sprintf("modifier :%s: %v", name, err),
				})
			}
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

func splitRawModifier(raw string) (name, value string) {
	if i := strings.IndexByte(raw, '='); i > 0 {
		return raw[:i], raw[i+1:]
	}
	return raw, ""
}
