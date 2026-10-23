package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
	"golang.org/x/mod/module"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
)

// RequirementChange describes a direct declaration in the named source file.
// Skipped declarations have a Reason; no provider metadata is persisted.
type RequirementChange struct {
	Import, Selector, Current, Latest, Target, Reason string
	Line                                              int
}

// RequirementSourceResult is an inspection or a completed source edit.
type RequirementSourceResult struct {
	Changes []RequirementChange
	Changed bool
	Backup  string
}

const (
	requireCheck  = "check"
	requireUpdate = "update"
	requirePin    = "pin"
)

type requirementQuery func(context.Context, require.Requirement, string) (string, error)

// ManageRequirementSource shares parsing, provider selection and writing across
// check, update and pin. The supplied directive table preserves CLI extensions.
// Only direct declarations in file are edited; imported sources are untouched.
func ManageRequirementSource(ctx context.Context, file, operation, pattern string, table *compiler.DirectiveTable) (RequirementSourceResult, error) {
	result, err := manageRequirementSource(ctx, file, operation, pattern, table, nil)
	if err != nil {
		return result, xerr.Validation("require "+operation, err)
	}
	return result, nil
}

func manageRequirementSource(ctx context.Context, file, operation, pattern string, table *compiler.DirectiveTable, query requirementQuery) (RequirementSourceResult, error) {
	var result RequirementSourceResult
	if operation != requireCheck && operation != requireUpdate && operation != requirePin {
		return result, fmt.Errorf("unknown requirement operation %q", operation)
	}
	if pattern != "" && pattern != "*" {
		if _, err := path.Match(pattern, ""); err != nil {
			return result, fmt.Errorf("invalid package pattern: %w", err)
		}
	}
	root, name, err := requirementSourceRoot(file)
	if err != nil {
		return result, err
	}
	defer func() { _ = root.Close() }()
	original, mode, err := readRequirementSource(root, name)
	if err != nil {
		return result, err
	}
	absolute, err := filepath.Abs(file)
	if err != nil {
		return result, err
	}
	if table == nil {
		table = native.Directives()
	}
	_, meta, err := compiler.Preprocess(ctx, table, string(original), absolute)
	if err != nil {
		return result, err
	}
	declarations := require.DirectDeclarations(meta)
	driverRoot, driverVersion := resolveDriverInfo()
	flowDir := filepath.Dir(absolute)
	gowork := findGoWork(flowDir)
	if query == nil {
		query = func(ctx context.Context, r require.Requirement, selector string) (string, error) {
			_, version, queryErr := resolveRemotePackageProvider(ctx, bundleImportPath(r), selector, driverRoot, driverVersion, gowork)
			return version, queryErr
		}
	}
	plan := requirementSourcePlan{file: file, operation: operation, pattern: pattern, driverRoot: driverRoot, gowork: gowork, flowDir: flowDir, query: query}
	result, updated, err := plan.apply(ctx, declarations, original)
	if err != nil {
		return RequirementSourceResult{}, err
	}

	if operation == requireCheck || !result.Changed {
		return result, nil
	}
	// Resolve everything before writing; protect edits made while Go was querying.
	current, _, err := readRequirementSource(root, name)
	if err != nil {
		return RequirementSourceResult{}, err
	}
	if !bytes.Equal(current, original) {
		return RequirementSourceResult{}, errors.New("source changed during resolution; retry the command")
	}
	backup := name + ".bak"
	if err := publishRequirementSource(ctx, root, name, backup, original, updated, mode); err != nil {
		return RequirementSourceResult{}, err
	}
	result.Backup = file + ".bak"
	return result, nil
}

type requirementSourcePlan struct {
	file, operation, pattern, driverRoot, gowork, flowDir string
	query                                                 requirementQuery
}

func (p requirementSourcePlan) apply(ctx context.Context, declarations []require.SourceDeclaration, original []byte) (RequirementSourceResult, []byte, error) {
	var result RequirementSourceResult
	lines := strings.Split(string(original), "\n")
	matched := 0
	for i := range declarations {
		declaration := &declarations[i]
		r := declaration.Requirement
		if !matchesRequirementPackage(p.pattern, r.Import) {
			continue
		}
		matched++
		change := RequirementChange{Import: r.Import, Selector: r.Version, Line: declaration.Line, Reason: p.skipReason(r)}
		if change.Reason == "" {
			var err error
			change, err = inspectRequirementSelector(ctx, r, change, p.operation, p.query)
			if err != nil {
				return RequirementSourceResult{}, nil, fmt.Errorf("%s:%d: @require %s: %w", p.file, declaration.Line, r.Import, err)
			}
			if change.Target != r.Version {
				result.Changed = true
				if p.operation != requireCheck {
					if replaceErr := replaceRequirementSelector(lines, *declaration, change.Target); replaceErr != nil {
						return RequirementSourceResult{}, nil, replaceErr
					}
				}
			}
		}
		result.Changes = append(result.Changes, change)
	}
	if p.pattern != "" && matched == 0 {
		return RequirementSourceResult{}, nil, fmt.Errorf("no direct requirements match package pattern %q", p.pattern)
	}
	if err := ctx.Err(); err != nil {
		return RequirementSourceResult{}, nil, err
	}
	return result, []byte(strings.Join(lines, "\n")), nil
}

func (p requirementSourcePlan) skipReason(r require.Requirement) string {
	switch {
	case r.IsLocal():
		return "local provider"
	case len(filterExternalRequires([]require.Requirement{r})) == 0:
		return "built-in provider"
	case unversionedPackageProvider(bundleImportPath(r), p.driverRoot, p.gowork, p.flowDir) != "":
		return "local/workspace provider"
	default:
		return ""
	}
}

func matchesRequirementPackage(pattern, name string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	matched, err := path.Match(pattern, name)
	return err == nil && matched
}

func inspectRequirementSelector(ctx context.Context, r require.Requirement, change RequirementChange, operation string, query requirementQuery) (RequirementChange, error) {
	selector := r.Version
	if selector == "" {
		selector = "latest"
	}
	var err error
	if operation != requireUpdate {
		change.Current, err = query(ctx, r, selector)
		if err != nil {
			return change, err
		}
		if module.CanonicalVersion(change.Current) != change.Current || change.Current == "" {
			return change, fmt.Errorf("provider returned non-canonical version %q", change.Current)
		}
	}
	if operation != requirePin {
		if selector == "latest" && operation == requireCheck {
			change.Latest = change.Current
		} else {
			change.Latest, err = query(ctx, r, "latest")
			if err != nil {
				return change, err
			}
		}
		if module.CanonicalVersion(change.Latest) != change.Latest || change.Latest == "" {
			return change, fmt.Errorf("provider returned non-canonical version %q", change.Latest)
		}
		change.Target = change.Latest
	} else {
		change.Target = change.Current
	}
	return change, nil
}

func replaceRequirementSelector(lines []string, declaration require.SourceDeclaration, version string) error {
	index := declaration.Line - 1
	if index < 0 || index >= len(lines) {
		return errors.New("invalid requirement source line")
	}
	line := lines[index]
	if declaration.SourceLine != line {
		return errors.New("requirement header changed by preprocessing; use a literal import and selector before editing")
	}
	start, end := declaration.SelectorStart, declaration.SelectorEnd
	if start < 0 || end < start || end > len(line) {
		return errors.New("invalid requirement selector span")
	}
	replacement := version
	if start == end {
		replacement = " " + version
	} else if line[start] == '\'' || line[start] == '"' {
		replacement = string(line[start]) + version + string(line[start])
	}
	lines[index] = line[:start] + replacement + line[end:]
	return nil
}

func requirementSourceRoot(file string) (*xfs.Root, string, error) {
	absolute, err := filepath.Abs(file)
	if err != nil {
		return nil, "", err
	}
	root, err := xfs.OpenRoot(filepath.Dir(absolute))
	return root, filepath.Base(absolute), err
}

func readRequirementSource(root *xfs.Root, name string) (data []byte, mode os.FileMode, err error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("%s must be a regular file (not a symlink)", name)
	}
	data, err = root.ReadFile(name)
	return data, info.Mode().Perm(), err
}

func publishRequirementSource(ctx context.Context, root *xfs.Root, name, backup string, original, updated []byte, mode os.FileMode) error {
	var previous []byte
	var previousMode os.FileMode
	previous, previousMode, err := readRequirementSource(root, backup)
	hadBackup := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.WriteFileAtomic(backup, original, 0o600); err != nil {
		return err
	}
	if err := root.WriteFileAtomic(name, updated, mode); err != nil {
		// Best effort restore of the previous undo stage if source publication fails.
		var restoreErr error
		if hadBackup {
			restoreErr = root.WriteFileAtomic(backup, previous, previousMode)
		} else {
			restoreErr = root.Remove(backup)
		}
		if restoreErr != nil {
			return fmt.Errorf("write source: %w; restoring prior backup failed: %w", err, restoreErr)
		}
		return err
	}
	return nil
}

// ResetRequirementSource restores the last pre-change bytes and consumes the
// optional backup. No resolver, lockfile, or Go metadata is involved.
func ResetRequirementSource(ctx context.Context, file string) error {
	if err := resetRequirementSource(ctx, file); err != nil {
		return xerr.Validation("require reset", err)
	}
	return nil
}

func resetRequirementSource(ctx context.Context, file string) error {
	root, name, err := requirementSourceRoot(file)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	_, mode, err := readRequirementSource(root, name)
	if err != nil {
		return err
	}
	backup := name + ".bak"
	previous, _, err := readRequirementSource(root, backup)
	if os.IsNotExist(err) {
		return fmt.Errorf("no previous source stage: %s", file+".bak")
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.WriteFileAtomic(name, previous, mode); err != nil {
		return err
	}
	if err := root.Remove(backup); err != nil {
		return fmt.Errorf("source restored but backup could not be removed: %w", err)
	}
	return nil
}
