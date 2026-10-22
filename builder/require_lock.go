package builder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/nexssp/flow/extensions/require"
)

const (
	requirementLockFormat   = 1
	maxRequirementLockBytes = 16 << 20
)

// requirementLock is a resolution result, not another requirement syntax.
// Selectors and options are bound by Input; replay never queries moving refs.
type requirementLock struct {
	Format       int                   `json:"format"`
	Go           string                `json:"go"`
	Flow         string                `json:"flow"`
	Toolchain    string                `json:"toolchain"`
	GoMod        string                `json:"goMod"`
	Input        string                `json:"input"`
	GoSum        string                `json:"goSum"`
	Requirements []require.Requirement `json:"requirements"`
	Modules      []lockedModule        `json:"modules"`
	LooseBundles []lockedLooseBundle   `json:"looseBundles,omitempty"`
}

type lockedModule struct {
	Path     string `json:"path"`
	Version  string `json:"version,omitempty"`
	Sum      string `json:"sum,omitempty"`
	GoModSum string `json:"goModSum,omitempty"`
	// Local replacements are checked by content; they are never vendored.
	Fingerprint string        `json:"fingerprint,omitempty"`
	Replace     *lockedModule `json:"replace,omitempty"`
}

// Loose bundles live in the generated main module, so Go's module graph does
// not cover them. Bind their source directories separately using the same
// content fingerprint as local replacement modules and harness cache keys.
type lockedLooseBundle struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}

var errRequirementLockAbsent = errors.New("requirement lock not found")

type listedLockModule struct {
	Path     string            `json:"Path"`
	Version  string            `json:"Version"`
	Sum      string            `json:"Sum"`
	GoModSum string            `json:"GoModSum"`
	Dir      string            `json:"Dir"`
	Main     bool              `json:"Main"`
	Replace  *listedLockModule `json:"Replace"`
	Error    *struct {
		Err string `json:"Err"`
	} `json:"Error"`
}

// FreezeRequirements resolves selectors through the same Go package-provider
// path as run/build, then atomically publishes the complete selected graph.
// pin is a CLI compatibility alias for this operation, not a source rewriter.
func FreezeRequirements(ctx context.Context, path string, sourceRequires func(context.Context, string) ([]require.Requirement, error)) (string, error) {
	lockPath, err := freezeRequirements(ctx, path, sourceRequires)
	if err != nil {
		return "", xerr.Validation("freeze requirements", err)
	}
	return lockPath, nil
}

func freezeRequirements(ctx context.Context, path string, sourceRequires func(context.Context, string) ([]require.Requirement, error)) (string, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if sourceRequires == nil {
		sourceRequires = SourceRequires
	}
	reqs, err := sourceRequires(ctx, absolute)
	if err != nil {
		return "", err
	}
	sources, err := buildSources(ctx, absolute)
	if err != nil {
		return "", err
	}
	usedNative, err := nativeBundlesForSources(sources, reqs, false)
	if err != nil {
		return "", err
	}
	driverRoot, driverVersion := resolveDriverInfo()
	flowDir := filepath.Dir(absolute)
	goworkPath := findGoWork(flowDir)
	resolved, err := resolveFreezeRequirements(ctx, reqs, driverRoot, driverVersion, goworkPath, flowDir)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "nflow-freeze-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if writeErr := writeHarness(dir, driverRoot, driverVersion, goworkPath, usedNative, filterExternalRequires(resolved), flowDir); writeErr != nil {
		return "", writeErr
	}
	if prepareErr := prepareRequirementDependencies(ctx, dir, driverRoot, nil); prepareErr != nil {
		return "", prepareErr
	}
	lock, err := captureRequirementLock(ctx, dir, driverVersion, reqs, resolved)
	if err != nil {
		return "", err
	}
	return publishRequirementLock(ctx, absolute, lock)
}

func captureRequirementLock(ctx context.Context, dir, driverVersion string, reqs, resolved []require.Requirement) (*requirementLock, error) {
	modules, err := snapshotRequirementModules(ctx, dir)
	if err != nil {
		return nil, err
	}
	if err = runGoContext(ctx, dir, "mod", "verify"); err != nil {
		return nil, fmt.Errorf("verify resolved modules: %w", err)
	}
	// Record Go's final MVS-selected version, not the individual query result.
	for i := range resolved {
		// Preserve the original selector, including an omitted selector. Only
		// provider metadata belongs to resolution; aliases/options remain source-owned.
		resolved[i].Version = reqs[i].Version
		for _, m := range modules {
			if resolved[i].ModulePath == m.Path && !resolved[i].IsLocal() {
				resolved[i].ResolvedVersion = m.Version
			}
		}
	}
	input, err := requirementInput(reqs)
	if err != nil {
		return nil, err
	}
	lock := requirementLock{Format: requirementLockFormat, Go: runtime.Version(), Flow: driverVersion, Input: input, Requirements: resolved, Modules: modules, LooseBundles: snapshotLooseBundles(resolved)}
	moduleData, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil, err
	}
	lock.GoMod = string(moduleData)
	lock.Toolchain, err = requirementToolchainInDirectory(ctx, dir)
	if err != nil {
		return nil, err
	}
	checksumData, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil {
		return nil, err
	}
	lock.GoSum = string(checksumData)
	if validationErr := validateRequirementLock(&lock); validationErr != nil {
		return nil, validationErr
	}
	return &lock, nil
}

func publishRequirementLock(ctx context.Context, absolute string, lock *requirementLock) (string, error) {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return "", err
	}
	if len(data) >= maxRequirementLockBytes {
		return "", fmt.Errorf("dependency lock exceeds %d bytes", maxRequirementLockBytes)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	// Rooted atomic replacement also refuses a symlink escaping this directory.
	root, err := xfs.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(absolute) + ".lock"
	if err := root.WriteFileAtomic(name, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return absolute + ".lock", nil
}

func requirementInput(reqs []require.Requirement) (string, error) {
	data, err := json.Marshal(reqs)
	if err != nil {
		return "", fmt.Errorf("encode requirement input: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func readRequirementLock(path string, reqs []require.Requirement) (*requirementLock, error) {
	f, err := os.Open(path + ".lock")
	if errors.Is(err, os.ErrNotExist) {
		return nil, errRequirementLockAbsent
	}
	if err != nil {
		return nil, fmt.Errorf("read requirement lock: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("requirement lock must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRequirementLockBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRequirementLockBytes {
		return nil, fmt.Errorf("requirement lock exceeds %d bytes", maxRequirementLockBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var lock requirementLock
	if decodeErr := decoder.Decode(&lock); decodeErr != nil {
		return nil, fmt.Errorf("invalid requirement lock: %w", decodeErr)
	}
	if trailingErr := decoder.Decode(new(any)); trailingErr != io.EOF {
		return nil, errors.New("invalid requirement lock: trailing data")
	}
	if validationErr := validateRequirementLock(&lock); validationErr != nil {
		return nil, validationErr
	}
	input, err := requirementInput(reqs)
	if err != nil {
		return nil, err
	}
	if lock.Input != input {
		return nil, fmt.Errorf("requirement lock is stale: declarations changed; run nflow require freeze %s", path)
	}
	if runtimeErr := validateRequirementRuntime(&lock, runtime.Version(), os.Getenv(HarnessEnv) != ""); runtimeErr != nil {
		return nil, runtimeErr
	}
	driverVersion := requirementDriverVersion()
	if lock.Flow != driverVersion {
		return nil, errors.New("requirement lock Flow driver changed; use the locked driver or explicitly refreeze")
	}
	if len(lock.Requirements) != len(reqs) {
		return nil, errors.New("requirement lock declaration count mismatch")
	}
	for i := range reqs {
		original := reqs[i]
		resolved := lock.Requirements[i]
		original.PackagePath = resolved.PackagePath
		original.ModulePath = resolved.ModulePath
		original.ResolvedVersion = resolved.ResolvedVersion
		if !reflect.DeepEqual(original, resolved) {
			return nil, fmt.Errorf("requirement lock declaration mismatch for %s", original.Import)
		}
	}
	return &lock, nil
}

func validateRequirementLock(lock *requirementLock) error {
	if lock.Format != requirementLockFormat {
		return fmt.Errorf("unsupported requirement lock format %d", lock.Format)
	}
	if lock.Go == "" || len(lock.Input) != 64 {
		return errors.New("invalid requirement lock header")
	}
	if len(lock.Modules) == 0 {
		return errors.New("requirement lock has no module graph")
	}
	lockedMod, err := modfile.Parse("requirement lock go.mod", []byte(lock.GoMod), nil)
	if err != nil || lockedMod.Module == nil || lockedMod.Module.Mod.Path != "nflow-harness" || lockedMod.Go == nil || lock.Toolchain == "" {
		return errors.New("invalid requirement lock build context")
	}
	if err := validateLockedGoSum(lock.GoSum); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, m := range lock.Modules {
		if seen[m.Path] {
			return fmt.Errorf("requirement lock has duplicate module %q", m.Path)
		}
		seen[m.Path] = true
		if err := validateLockedModule(m, lock.GoSum); err != nil {
			return err
		}
	}
	for i := range lock.Requirements {
		r := &lock.Requirements[i]
		if r.IsLocal() || len(filterExternalRequires([]require.Requirement{*r})) == 0 {
			continue
		}
		if r.PackagePath == "" || !seen[r.ModulePath] {
			return fmt.Errorf("locked requirement %s has no provider", r.Import)
		}
	}
	return nil
}

// resolveRequirementsForFlow is the single run/build entry to lock replay or
// the existing resolver. A present-but-invalid lock never falls back to latest.
func resolveRequirementsForFlow(ctx context.Context, reqs []require.Requirement, path, driverRoot, driverVersion, goworkPath string) ([]require.Requirement, *requirementLock, error) {
	lock, err := readRequirementLock(path, reqs)
	if err != nil && !errors.Is(err, errRequirementLockAbsent) {
		return nil, nil, err
	}
	if lock != nil {
		return append([]require.Requirement(nil), lock.Requirements...), lock, nil
	}
	resolved, err := resolveRequirementPackagesWithContext(ctx, reqs, driverRoot, driverVersion, goworkPath, flowDirectory(path))
	return resolved, nil, err
}

// prepareRequirementDependencies is shared by freeze, harness and standalone
// builds. The lock is seeded BEFORE tidy and verified BEFORE compilation.
func prepareRequirementDependencies(ctx context.Context, dir, driverRoot string, lock *requirementLock) error {
	if lock != nil {
		if err := validateLockedLocalModules(lock); err != nil {
			return err
		}
		if err := seedRequirementLock(dir, lock); err != nil {
			return err
		}
	}
	if err := runGoContext(ctx, dir, "mod", "tidy", "-e"); err != nil {
		return err
	}
	if err := retainDriverRequirements(ctx, dir, driverRoot); err != nil {
		return err
	}
	if lock == nil {
		return nil
	}
	toolchain, err := requirementToolchainInDirectory(ctx, dir)
	if err != nil {
		return err
	}
	if toolchain != lock.Toolchain {
		return fmt.Errorf("requirement lock build toolchain mismatch: locked %s, selected %s", lock.Toolchain, toolchain)
	}
	actual, err := snapshotRequirementModules(ctx, dir)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, lock.Modules) {
		return errors.New("requirement lock mismatch: selected graph, local content, or checksums changed; explicitly run nflow require freeze to update")
	}
	sums, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil {
		return err
	}
	if string(sums) != lock.GoSum {
		return errors.New("requirement lock mismatch: Go checksum metadata changed; explicitly refreeze")
	}
	if err := runGoContext(ctx, dir, "mod", "verify"); err != nil {
		return fmt.Errorf("verify locked modules: %w", err)
	}
	return nil
}

func seedRequirementLock(dir string, lock *requirementLock) error {
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	mod, err := modfile.Parse(path, data, nil)
	if err != nil {
		return err
	}
	lockedMod, err := modfile.Parse("requirement lock go.mod", []byte(lock.GoMod), nil)
	if err != nil {
		return err
	}
	if lockedMod.Go == nil {
		return errors.New("requirement lock lacks a Go directive")
	}
	if goStmtErr := mod.AddGoStmt(lockedMod.Go.Version); goStmtErr != nil {
		return goStmtErr
	}
	if lockedMod.Toolchain != nil {
		if toolchainErr := mod.AddToolchainStmt(lockedMod.Toolchain.Name); toolchainErr != nil {
			return toolchainErr
		}
	} else {
		mod.DropToolchainStmt()
	}
	for _, m := range lock.Modules {
		if requireErr := mod.AddRequire(m.Path, m.Version); requireErr != nil {
			return requireErr
		}
	}
	data, err = mod.Format()
	if err != nil {
		return err
	}
	root, err := xfs.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if writeErr := root.WriteFileAtomic("go.mod", data, 0o600); writeErr != nil {
		return writeErr
	}
	return root.WriteFileAtomic("go.sum", []byte(lock.GoSum), 0o600)
}

func snapshotRequirementModules(ctx context.Context, dir string) ([]lockedModule, error) {
	if err := runGoContext(ctx, dir, "mod", "download", "all"); err != nil {
		return nil, err
	}
	data, err := runGoOutputContext(ctx, dir, "list", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	var modules []lockedModule
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var m listedLockModule
		err := decoder.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode module graph: %w", err)
		}
		if m.Error != nil {
			return nil, fmt.Errorf("module graph: %s", m.Error.Err)
		}
		if m.Main {
			continue
		}
		item := lockedModule{Path: m.Path, Version: m.Version, Sum: m.Sum, GoModSum: m.GoModSum}
		if m.Replace != nil {
			r := m.Replace
			item.Replace = &lockedModule{Path: r.Path, Version: r.Version, Sum: r.Sum, GoModSum: r.GoModSum}
			if r.Version == "" {
				item.Replace.Fingerprint = moduleSourceFingerprint(r.Dir)
			}
		}
		modules = append(modules, item)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return modules, nil
}

func requirementLockDigest(lock *requirementLock) (string, error) {
	if lock == nil {
		return "", nil
	}
	data, err := json.Marshal(lock)
	if err != nil {
		return "", fmt.Errorf("encode requirement lock: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func requirementToolchainInDirectory(ctx context.Context, dir string) (string, error) {
	data, err := runGoOutputContext(ctx, dir, "env", "GOVERSION")
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", errors.New("go env GOVERSION returned no toolchain")
	}
	return version, nil
}

func requirementBuildToolchain(ctx context.Context, moduleDefinition string) (string, error) {
	dir, err := os.MkdirTemp("", "nflow-toolchain-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(moduleDefinition), 0o600); err != nil {
		return "", err
	}
	return requirementToolchainInDirectory(ctx, dir)
}

// In a generated harness Flow is a dependency, not the main Go module.
func requirementDriverVersion() string {
	root, version := resolveDriverInfo()
	if root != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range info.Deps {
			if dependency.Path == flowRootModulePath {
				if dependency.Replace != nil && dependency.Replace.Version == "" {
					return ""
				}
				return dependency.Version
			}
		}
	}
	return version
}

// ValidateRequirementLock checks a present lock even for native-only execution
// and cache hits, where no module staging/build takes place.
func ValidateRequirementLock(ctx context.Context, path string, reqs []require.Requirement) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := readRequirementLock(path, reqs)
	if errors.Is(err, errRequirementLockAbsent) {
		return nil
	}
	if err == nil && lock != nil {
		err = validateLockedLocalModules(lock)
	}
	if err != nil {
		return xerr.Validation("requirement lock", err)
	}
	return nil
}

func validateLockedLocalModules(lock *requirementLock) error {
	if !reflect.DeepEqual(snapshotLooseBundles(lock.Requirements), lock.LooseBundles) {
		return errors.New("requirement lock mismatch: loose local bundle content changed or is not fingerprinted; explicitly refreeze")
	}
	for _, m := range lock.Modules {
		if m.Replace != nil && m.Replace.Version == "" {
			if moduleSourceFingerprint(m.Replace.Path) != m.Replace.Fingerprint {
				return fmt.Errorf("requirement lock mismatch: local module %s changed; explicitly refreeze", m.Path)
			}
		}
	}
	return nil
}

func validLockedChecksum(checksum string) bool {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(checksum, "h1:"))
	return strings.HasPrefix(checksum, "h1:") && err == nil && len(raw) == sha256.Size
}

func validateLockedGoSum(data string) error {
	seen := map[string]bool{}
	for line := range strings.SplitSeq(data, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !validLockedChecksum(fields[2]) {
			return errors.New("invalid requirement lock go.sum entry")
		}
		if err := module.Check(fields[0], strings.TrimSuffix(fields[1], "/go.mod")); err != nil {
			return fmt.Errorf("invalid requirement lock go.sum module: %w", err)
		}
		key := fields[0] + " " + fields[1]
		if seen[key] {
			return fmt.Errorf("duplicate requirement lock go.sum entry: %s", key)
		}
		seen[key] = true
	}
	return nil
}

// The parent CLI and its generated runner may be compiled by different Go
// toolchains. Compare each executable to its own recorded build context.
func validateRequirementRuntime(lock *requirementLock, current string, harness bool) error {
	expected := lock.Go
	role := "CLI runtime"
	if harness {
		expected, role = lock.Toolchain, "harness runtime"
	}
	if expected != current {
		return fmt.Errorf("requirement lock %s uses %s, current runtime is %s; use the locked toolchain or explicitly refreeze", role, expected, current)
	}
	return nil
}

func snapshotLooseBundles(reqs []require.Requirement) []lockedLooseBundle {
	var bundles []lockedLooseBundle
	seen := map[string]bool{}
	for i := range reqs {
		r := &reqs[i]
		if !r.IsLoose || seen[r.LocalPath] {
			continue
		}
		seen[r.LocalPath] = true
		bundles = append(bundles, lockedLooseBundle{Path: r.LocalPath, Fingerprint: moduleSourceFingerprint(r.LocalPath)})
	}
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].Path < bundles[j].Path })
	return bundles
}

func resolveFreezeRequirements(ctx context.Context, reqs []require.Requirement, driverRoot, driverVersion, goworkPath, flowDir string) ([]require.Requirement, error) {
	// A bare external target means latest only during an explicit freeze.
	// Same-module/workspace imports keep the existing provider precedence.
	candidates := append([]require.Requirement(nil), reqs...)
	for i := range candidates {
		r := &candidates[i]
		if len(filterExternalRequires([]require.Requirement{*r})) == 0 || r.IsLocal() || r.Version != "" {
			continue
		}
		if unversionedPackageProvider(bundleImportPath(*r), driverRoot, goworkPath, flowDir) == "" {
			r.Version = "latest"
		}
	}
	return resolveRequirementPackagesWithContext(ctx, candidates, driverRoot, driverVersion, goworkPath, flowDir)
}

func validateLockedModule(m lockedModule, goSum string) error {
	if err := module.CheckPath(m.Path); err != nil {
		return fmt.Errorf("invalid locked module: %w", err)
	}
	if !semver.IsValid(m.Version) {
		return fmt.Errorf("invalid locked version for %s: %q", m.Path, m.Version)
	}
	effective := m
	if m.Replace != nil {
		effective = *m.Replace
		if effective.Replace != nil {
			return errors.New("nested replacement in requirement lock")
		}
	}
	if effective.Version == "" {
		if effective.Fingerprint == "" || !filepath.IsAbs(effective.Path) {
			return fmt.Errorf("invalid local replacement for %s", m.Path)
		}
	} else {
		if err := module.Check(effective.Path, effective.Version); err != nil {
			return fmt.Errorf("invalid locked provider: %w", err)
		}
		for suffix, checksum := range map[string]string{"": effective.Sum, "/go.mod": effective.GoModSum} {
			if !validLockedChecksum(checksum) || !strings.Contains("\n"+goSum, "\n"+effective.Path+" "+effective.Version+suffix+" "+checksum+"\n") {
				return fmt.Errorf("missing or invalid checksum for %s@%s", effective.Path, effective.Version)
			}
		}
	}
	return nil
}
