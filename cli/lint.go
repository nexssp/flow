package cli

import (
	"context"
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
	inv, err := newInvocation(nil, nil)
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

	// Preserve the existing explicit single-file path, including its
	// source-specific harness and error behavior.
	if len(args) == 1 && !isRecursiveLintPattern(args[0]) {
		info, err := os.Stat(args[0])
		if err != nil || !info.IsDir() {
			return withHarness(ctx, inv, "lint", args[0], args, runLintInProcess)
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
		reqs, reqErr := inv.sourceRequiresFromFile(ctx, path)
		if reqErr == nil {
			allRequirements = append(allRequirements, reqs...)
		}
	}
	if os.Getenv(harnessEnv) != "" || !requirementsNeedExternalHarness(inv, allRequirements) {
		return runLintBatchForInvocation(ctx, inv, files)
	}
	bin, err := EnsureHarnessContext(ctx, allRequirements, files[0])
	if err != nil {
		return runLintBatchForInvocationWithHarnessError(ctx, inv, files, err)
	}
	return ExecHarnessContext(ctx, bin, append([]string{"lint"}, files...))
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

func runLintInProcess(ctx context.Context, inv *invocation, args []string) int {
	if len(args) < 1 {
		return fatalf("usage: nflow lint <file.nflow>")
	}
	path := args[0]

	src, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read: %v", err)
	}

	reqs, err := inv.sourceRequiresFromFile(ctx, path)
	if err != nil {
		return fatalf("requires: %v", err)
	}

	cfg, err := inv.buildConfig(reqs)
	if err != nil {
		return fatalf("config: %v", err)
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
	inv, err := newInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(context.Background(), inv, func(ctx context.Context) int {
		return runLintBatchForInvocationWithHarnessError(ctx, inv, paths, harnessErr)
	})
}

func runLintBatchForInvocation(ctx context.Context, inv *invocation, paths []string) int {
	return runLintBatchForInvocationWithHarnessError(ctx, inv, paths, nil)
}

func runLintBatchForInvocationWithHarnessError(ctx context.Context, inv *invocation, paths []string, harnessErr error) int {
	var issues []LintIssue
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, LintIssue{File: path, Kind: "read", Message: err.Error()})
			continue
		}

		reqs, reqErr := inv.sourceRequiresFromFile(ctx, path)
		if reqErr != nil {
			// lintFile reports preprocessing/compile failures as source diagnostics;
			// do not turn one bad source into an early batch abort.
			reqs = nil
		}
		if harnessErr != nil && requirementsNeedExternalHarness(inv, reqs) {
			issues = append(issues, LintIssue{
				File:    path,
				Kind:    "config",
				Message: fmt.Sprintf("external harness unavailable: %v", harnessErr),
			})
			continue
		}
		cfg, err := inv.buildConfig(reqs)
		if err != nil {
			issues = append(issues, LintIssue{File: path, Kind: "config", Message: err.Error()})
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

// computeLineMods returns the line-indexed modifier lookups a source
// with the given meta would install. The explain command uses it to render
// inherited modifier state without building a program.
func computeLineMods(cfg runner.Config, meta map[string]any) []core.LineLookup {
	opts := append([]core.CompileOption(nil), cfg.CompileOpts...)
	contribs := core.PreprocessContributionsFromMeta(meta, cfg.CompileOpts...)
	opts = append(opts, contribs.CompileOpts...)
	return core.LineModifiersFromOptions(cfg.Modifiers, opts)
}

// lintFile returns one compiler diagnostic for a source. The compiler owns
// parsing, analysis, schemas, modifiers, and extension contributions; runner
// owns the same materialization path used by execution.
func lintFile(path, src string, cfg runner.Config) []LintIssue {
	return lintFileWithContext(context.Background(), path, src, cfg)
}

func lintFileWithContext(ctx context.Context, path, src string, cfg runner.Config) []LintIssue {
	if _, err := runner.Compile(ctx, cfg, src, path); err != nil {
		return []LintIssue{{
			File:    path,
			Kind:    "compile",
			Message: err.Error(),
		}}
	}
	return nil
}
