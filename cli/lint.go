package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nexssp/flow/builder"
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/runner"
)

type LintIssue struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

const lintIssueCompile = "compile"

func runLint(args []string) int {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(context.Background(), inv, func(ctx context.Context) int {
		return runLintInInvocation(ctx, inv, args)
	})
}

func runLintInInvocation(ctx context.Context, inv *invocation, args []string) int {
	if len(args) == 0 {
		return fatalf("usage: nflow lint <file.nflow | directory | ./...>")
	}

	if shouldLintSingleTarget(args) {
		return withHarness(ctx, inv, "lint", args[0], args, runLintInProcess)
	}

	files, err := discoverLintFiles(args)
	if err != nil {
		return fatalf("%v", err)
	}

	allRequirements, requirementErrors := inspectLintRequirements(ctx, inv, files)
	needsExternalHarness := requirementsNeedExternalHarness(inv, allRequirements)
	if os.Getenv(harnessEnv) != "" || !needsExternalHarness {
		return runLintBatchForInvocation(ctx, inv, files, requirementErrors)
	}
	bin, err := builder.EnsureHarnessCLIContext(ctx, allRequirements, files...)
	if err != nil {
		return runLintBatchForInvocationWithHarnessError(ctx, inv, files, err, requirementErrors)
	}
	return builder.ExecHarnessContext(ctx, bin, append([]string{"lint"}, files...))
}

func shouldLintSingleTarget(args []string) bool {
	if len(args) != 1 || isRecursiveLintPattern(args[0]) {
		return false
	}
	info, err := os.Stat(args[0])
	if err != nil || !info.IsDir() {
		return true
	}
	return false
}

func inspectLintRequirements(ctx context.Context, inv *invocation, files []string) (allRequirements []require.Requirement, requirementErrors map[string]error) {
	requirementErrors = make(map[string]error)
	for _, path := range files {
		reqs, requireErr := inv.requirementsForHarness(ctx, path)
		if requireErr != nil {
			requirementErrors[path] = requireErr
			continue
		}
		allRequirements = append(allRequirements, reqs...)
	}
	return allRequirements, requirementErrors
}

var lintDiscoveryExcludedDirs = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	"vendor":       {},
	"node_modules": {},
	"testdata":     {},
}

func isRecursiveLintPattern(target string) bool {
	return target == "..." || strings.HasSuffix(target, "/...") ||
		(filepath.Separator == '\\' && strings.HasSuffix(target, `\...`))
}

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
				name := entry.Name()
				// Skip hidden dirs, vendor/git, and dirs starting with underscore (partials)
				if _, skip := lintDiscoveryExcludedDirs[name]; skip || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		// Skip non-.nflow, symlinks, partials (_*.nflow), and flat companion files (*--*.nflow)
		name := entry.Name()
		if entry.Type()&fs.ModeSymlink != 0 ||
			!strings.HasSuffix(name, ".nflow") ||
			strings.HasPrefix(name, "_") ||
			isLintFlatFixtureCompanion(path) {
			return nil
		}
		found++
		add(path)
		return nil
	})
	return found, err
}

func isLintFlatFixtureCompanion(path string) bool {
	name := strings.TrimSuffix(filepath.Base(path), ".nflow")
	for name != "" {
		separator := strings.Index(name, "--")
		if separator < 0 {
			return false
		}
		fixture := filepath.Join(filepath.Dir(path), name[:separator]+".nflow")
		if info, err := os.Stat(fixture); err == nil && !info.IsDir() {
			return true
		}
		name = name[separator+2:]
	}
	return false
}

func runLintInProcess(ctx context.Context, inv *invocation, args []string) int {
	if len(args) < 1 {
		return fatalf("usage: nflow lint <file.nflow>")
	}
	path := args[0]

	src, err := readSourceFile(path)
	if err != nil {
		return fatalf("%v", err)
	}

	_, _, cfg, err := inv.prepareRequiredSource(ctx, string(src), path)
	if err != nil {
		return fatalf("requires: %v", err)
	}

	issues := lintFileWithContext(ctx, path, string(src), cfg)

	if len(issues) == 0 {
		fmt.Fprintln(os.Stdout, "ok")
		return 0
	}

	if err := writeJSON(os.Stdout, issues, true); err != nil {
		return fatalf("encode issues: %v", err)
	}
	return 1
}

func runLintBatchInProcessWithHarnessError(paths []string, harnessErr error) int {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(context.Background(), inv, func(ctx context.Context) int {
		return runLintBatchForInvocationWithHarnessError(ctx, inv, paths, harnessErr, nil)
	})
}

func runLintBatchForInvocation(ctx context.Context, inv *invocation, paths []string, requirementErrors map[string]error) int {
	return runLintBatchForInvocationWithHarnessError(ctx, inv, paths, nil, requirementErrors)
}

func runLintBatchForInvocationWithHarnessError(ctx context.Context, inv *invocation, paths []string, harnessErr error, requirementErrors map[string]error) int {
	var issues []LintIssue
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, LintIssue{File: path, Kind: "read", Message: err.Error()})
			continue
		}

		if reqErr, ok := requirementErrors[path]; ok {
			issues = append(issues, LintIssue{File: path, Kind: lintIssueCompile, Message: reqErr.Error()})
			continue
		}
		reqs, reqErr := inv.requirementsForHarness(ctx, path)
		if reqErr != nil {
			issues = append(issues, LintIssue{File: path, Kind: lintIssueCompile, Message: reqErr.Error()})
			continue
		}
		if harnessErr != nil && requirementsNeedExternalHarness(inv, reqs) {
			issues = append(issues, LintIssue{
				File:    path,
				Kind:    "config",
				Message: fmt.Sprintf("external harness unavailable: %v", harnessErr),
			})
			continue
		}
		if _, configErr := inv.buildConfig(reqs); configErr != nil {
			issues = append(issues, LintIssue{File: path, Kind: "config", Message: configErr.Error()})
			continue
		}
		_, _, cfg, err := inv.prepareRequiredSource(ctx, string(src), path)
		if err != nil {
			issues = append(issues, LintIssue{File: path, Kind: lintIssueCompile, Message: err.Error()})
			continue
		}
		issues = append(issues, lintFileWithContext(ctx, path, string(src), cfg)...)
	}

	if len(issues) == 0 {
		fmt.Fprintf(os.Stdout, "ok (%d files)\n", len(paths))
		return 0
	}
	if err := writeJSON(os.Stdout, issues, true); err != nil {
		return fatalf("encode issues: %v", err)
	}
	return 1
}

func computeLineMods(cfg runner.Config, meta map[string]any) []compiler.LineLookup {
	opts := append([]compiler.CompileOption(nil), cfg.CompileOpts...)
	contribs := compiler.PreprocessContributionsFromMeta(meta, cfg.CompileOpts...)
	opts = append(opts, contribs.CompileOpts...)
	return compiler.LineModifiersFromOptions(cfg.Modifiers, opts)
}

func lintFile(path, src string, cfg runner.Config) []LintIssue {
	return lintFileWithContext(context.Background(), path, src, cfg)
}

func lintFileWithContext(ctx context.Context, path, src string, cfg runner.Config) []LintIssue {
	if _, err := runner.Compile(ctx, cfg, src, path); err != nil {
		return []LintIssue{{
			File:    path,
			Kind:    lintIssueCompile,
			Message: err.Error(),
		}}
	}
	return nil
}
