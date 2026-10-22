package builder

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/kernel/xfs"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
)

const defaultGoCommandTimeout = 10 * time.Minute

const defaultAdapterDirectory = "nexssflow"

const maxCapturedOutputBytes = 8 * 1024

const harnessLockStaleAfter = 5 * time.Minute

// HarnessEnv marks a process as running inside an external-module harness.
const (
	HarnessEnv = "NFLOW_HARNESS"
	harnessEnv = HarnessEnv
)

func EnsureHarness(reqs []require.Requirement, flowPath ...string) (string, error) {
	return EnsureHarnessContext(context.Background(), reqs, flowPath...)
}

func EnsureHarnessContext(ctx context.Context, reqs []require.Requirement, flowPath ...string) (string, error) {
	external := filterExternalRequires(reqs)
	if len(external) == 0 {
		return "", errors.New("harness: no external modules to link")
	}

	if len(flowPath) == 0 {
		return "", errors.New("harness: missing flow path")
	}
	flowFile := flowPath[0]
	flowDir := flowDirectory(flowFile)

	sources, err := harnessSources(ctx, flowPath)
	if err != nil {
		return "", err
	}
	driverRoot, driverVersion := resolveDriverInfo()
	goworkPath := findGoWork(flowDir)
	inputs, err := resolveHarnessBuildInputs(
		ctx, reqs, flowFile, flowDir, driverRoot, driverVersion, goworkPath, sources,
	)
	if err != nil {
		return "", err
	}
	return buildHarnessRunner(ctx, inputs.key, inputs.external, driverRoot, driverVersion, goworkPath, inputs.usedNative, flowDir, inputs.dependencyLock)
}

func harnessSources(ctx context.Context, flowPath []string) ([]compiler.BuildSource, error) {
	var sources []compiler.BuildSource
	for _, path := range flowPath {
		fragments, err := buildSources(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("harness: inspect source %s: %w", path, err)
		}
		sources = append(sources, fragments...)
	}
	if len(flowPath) > 1 {
		for _, path := range flowPath {
			if _, err := os.Stat(path + ".lock"); err == nil || !errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("locked workflows must be linted individually, not in a multi-file harness")
			}
		}
	}
	return sources, nil
}

type harnessBuildInputs struct {
	external       []require.Requirement
	usedNative     []nativeBundleInfo
	dependencyLock *requirementLock
	key            string
}

func resolveHarnessBuildInputs(ctx context.Context, reqs []require.Requirement, flowFile, flowDir, driverRoot, driverVersion, goworkPath string, sources []compiler.BuildSource) (*harnessBuildInputs, error) {
	resolved, dependencyLock, err := resolveRequirementsForFlow(ctx, reqs, flowFile, driverRoot, driverVersion, goworkPath)
	if err != nil {
		return nil, err
	}
	external := filterExternalRequires(resolved)
	usedNative, err := nativeBundlesForSources(sources, reqs, false)
	if err != nil {
		return nil, fmt.Errorf("harness: bundle plan: %w", err)
	}
	key := harnessKey(external, goworkPath, usedNative...)
	moduleDefinition := harnessGoMod(driverRoot, driverVersion, goworkPath, external, flowDir)
	if dependencyLock != nil {
		err = validateLockedLocalModules(dependencyLock)
		if err != nil {
			return nil, err
		}
		moduleDefinition = dependencyLock.GoMod
	}
	toolchain, err := requirementBuildToolchain(ctx, moduleDefinition)
	if err != nil {
		return nil, err
	}
	if dependencyLock != nil && toolchain != dependencyLock.Toolchain {
		return nil, fmt.Errorf("requirement lock build toolchain mismatch: locked %s, selected %s", dependencyLock.Toolchain, toolchain)
	}
	digest, err := requirementLockDigest(dependencyLock)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(key + digest + toolchain))
	key = hex.EncodeToString(sum[:])[:16]
	return &harnessBuildInputs{
		external:       external,
		usedNative:     usedNative,
		dependencyLock: dependencyLock,
		key:            key,
	}, nil
}

func buildHarnessRunner(ctx context.Context, key string, external []require.Requirement, driverRoot, driverVersion, goworkPath string, usedNative []nativeBundleInfo, flowDir string, dependencyLock *requirementLock) (string, error) {
	dir, err := harnessCacheDir(key)
	if err != nil {
		return "", err
	}

	bin := filepath.Join(dir, "runner"+exeSuffix())
	if _, statErr := os.Stat(bin); statErr == nil {
		fmt.Fprintf(os.Stderr, "⚡ nflow: using cached runner [%s]\n", key[:8])
		return bin, nil
	}

	unlock, err := acquireHarnessLock(ctx, dir)
	if err != nil {
		return "", err
	}
	defer unlock()

	if _, statErr := os.Stat(bin); statErr == nil {
		fmt.Fprintf(os.Stderr, "⚡ nflow: using cached runner [%s]\n", key[:8])
		return bin, nil
	}

	logHarnessPreparation(external)

	buildDir, err := os.MkdirTemp("", "nflow-harness-")
	if err != nil {
		return "", fmt.Errorf("harness: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	if err := writeHarness(buildDir, driverRoot, driverVersion, goworkPath, usedNative, external, flowDir); err != nil {
		return "", err
	}

	tidyStep := startProgress("⚙️  nflow: resolving dependencies (go mod tidy -e)")
	tidyErr := prepareRequirementDependencies(ctx, buildDir, driverRoot, dependencyLock)
	if tidyErr != nil {
		tidyStep.Complete("failed")
		return "", explainBuildError(tidyErr, external)
	}
	tidyStep.Complete("done")

	tmp := bin + ".tmp." + strconv.Itoa(os.Getpid())
	buildStep := startProgress(fmt.Sprintf("🔨 nflow: compiling runner binary [%s]", key[:8]))
	buildErr := runGoContext(ctx, buildDir, "build", "-mod=readonly", "-trimpath", "-o", tmp, ".")
	if buildErr != nil {
		buildStep.Complete("failed")
		return "", explainBuildError(buildErr, external)
	}
	buildStep.Complete("done")

	if err := os.Rename(tmp, bin); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("harness: install: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✓ nflow: runner ready\n")
	return bin, nil
}

func logHarnessPreparation(external []require.Requirement) {
	fmt.Fprintf(os.Stderr, "📦 nflow: preparing harness for %d external module(s)\n", len(external))
	for i := range external {
		r := &external[i]
		switch {
		case r.IsLoose:
			fmt.Fprintf(os.Stderr, "   • loose package: %s (auto-bundling as %s)\n", r.LocalPath, r.LooseID)
		case r.IsLocal():
			fmt.Fprintf(os.Stderr, "   • local module: %s (root: %s)\n", r.Import, r.ModuleRoot)
		case r.Version == "" && r.ModulePath != "":
			fmt.Fprintf(os.Stderr, "   • same-module package: %s (module: %s)\n", r.PackagePath, r.ModulePath)
		default:
			fmt.Fprintf(os.Stderr, "   • remote module: %s@%s\n", r.Import, r.Version)
		}
	}
}

func retainDriverRequirements(ctx context.Context, buildDir, driverRoot string) error {
	if driverRoot == "" {
		return nil
	}
	driverRequirements, err := readModuleRequirements(filepath.Join(driverRoot, "go.mod"))
	if err != nil {
		return fmt.Errorf("harness: read driver go.mod: %w", err)
	}
	harnessRequirements, err := readModuleRequirements(filepath.Join(buildDir, "go.mod"))
	if err != nil {
		return fmt.Errorf("harness: read generated go.mod: %w", err)
	}
	for _, requirement := range requirementsToRetain(driverRequirements, harnessRequirements) {
		arg := "-require=" + requirement.path + "@" + requirement.version
		if err := runGoContext(ctx, buildDir, "mod", "edit", arg); err != nil {
			return fmt.Errorf("harness: retain driver module dependency %s: %w", requirement.path, err)
		}
	}
	if err := runGoContext(ctx, buildDir, "mod", "download"); err != nil {
		return fmt.Errorf("harness: download driver module dependencies: %w", err)
	}
	return nil
}

func requirementsToRetain(driverRequirements, harnessRequirements []moduleRequirement) []moduleRequirement {
	present := make(map[string]string, len(harnessRequirements))
	for _, requirement := range harnessRequirements {
		present[requirement.path] = requirement.version
	}

	var retained []moduleRequirement
	for _, requirement := range driverRequirements {
		selected := present[requirement.path]
		// Driver requirements are lower bounds, not forced downgrades of
		// dependencies selected by an external bundle through Go's MVS.
		if requirement.path == flowRootModulePath || (selected != "" && semver.Compare(selected, requirement.version) >= 0) {
			continue
		}
		retained = append(retained, requirement)
		present[requirement.path] = requirement.version
	}
	return retained
}

func acquireHarnessLock(ctx context.Context, dir string) (func(), error) {
	lockPath := filepath.Join(dir, "build.lock")

	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("harness: lock %s: %w", lockPath, err)
		}

		if info, statErr := os.Stat(lockPath); statErr == nil &&
			time.Since(info.ModTime()) > harnessLockStaleAfter {
			_ = os.Remove(lockPath)
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func flowDirectory(path string) string {
	if path == "" {
		if cwd, err := os.Getwd(); err == nil {
			return cwd
		}
		return "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Dir(path)
	}
	info, err := os.Stat(abs)
	if err == nil && info.IsDir() {
		return abs
	}
	return filepath.Dir(abs)
}

func filterExternalRequires(reqs []require.Requirement) []require.Requirement {
	out := make([]require.Requirement, 0, len(reqs))
	seen := make(map[string]bool, len(reqs))
	for i := range reqs {
		r := &reqs[i]
		targetID := require.NormalizeID(r.Import)
		if _, ok := compiler.Lookup(targetID); ok {
			continue
		}
		if _, ok := compiler.Lookup(r.Import); ok {
			continue
		}

		key := r.Import + "@" + r.Version
		if r.Alias != "" {
			key += "#" + r.Alias
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, *r)
	}
	return out
}

func ExecHarness(bin string, args []string) int {
	return ExecHarnessContext(context.Background(), bin, args)
}

func ExecHarnessContext(ctx context.Context, bin string, args []string) int {
	quiet := false
	for _, a := range args {
		if a == "--json" || a == "--quiet" {
			quiet = true
			break
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), harnessEnv+"=1")

	var harnessStep *progressTracker
	if !quiet {
		harnessStep = startProgress("▶ nflow: harness")
	}

	if err := cmd.Start(); err != nil {
		if harnessStep != nil {
			harnessStep.Complete("failed")
		}
		fmt.Fprintf(os.Stderr, "harness: exec: %v\n", err)
		return 1
	}
	if harnessStep != nil {
		harnessStep.Complete("running")
	}

	err := cmd.Wait()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return exit.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "harness: exec: %v\n", err)
		return 1
	}
	return 0
}

func harnessKey(reqs []require.Requirement, goworkPath string, usedNative ...nativeBundleInfo) string {
	sorted := append([]require.Requirement(nil), reqs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Import != sorted[j].Import {
			return sorted[i].Import < sorted[j].Import
		}
		if sorted[i].Version != sorted[j].Version {
			return sorted[i].Version < sorted[j].Version
		}
		return sorted[i].PackagePath < sorted[j].PackagePath
	})

	h := sha256.New()
	fmt.Fprintln(h, "resolver=go-package-provider-v1")
	for i := range sorted {
		r := &sorted[i]
		fmt.Fprintf(h, "%s@%s\n", r.Import, r.Version)
		fmt.Fprintf(h, "  package=%s\n  provider=%s@%s\n", r.PackagePath, r.ModulePath, r.ResolvedVersion)
		if r.Alias != "" {
			fmt.Fprintf(h, "  as=%s\n", r.Alias)
		}
		if len(r.Options) > 0 {
			optKeys := make([]string, 0, len(r.Options))
			for k := range r.Options {
				optKeys = append(optKeys, k)
			}
			sort.Strings(optKeys)
			for _, k := range optKeys {
				fmt.Fprintf(h, "  opt:%s=%s\n", k, r.Options[k])
			}
		}
		if r.IsLocal() {
			// Hash the provider selected by @require, not just its bundle package.
			// Sibling packages and module metadata are inputs to the same build.
			root := r.ModuleRoot
			if r.IsLoose || root == "" {
				root = r.LocalPath
			}
			fmt.Fprintf(h, "  fingerprint=%s\n", moduleSourceFingerprint(root))
		}
	}
	native := append([]nativeBundleInfo(nil), usedNative...)
	sort.Slice(native, func(i, j int) bool {
		if native[i].id != native[j].id {
			return native[i].id < native[j].id
		}
		return native[i].pkg < native[j].pkg
	})
	for i := range native {
		fmt.Fprintf(h, "native=%s package=%s\n", native[i].id, native[i].pkg)
	}
	fmt.Fprintf(h, "go=%s\n", runtime.Version())
	fmt.Fprintf(h, "nflow=%s\n", Version)

	if goworkPath != "" {
		fmt.Fprintf(h, "gowork=%s\n", goworkPath)
		if data, err := os.ReadFile(goworkPath); err == nil {
			sum := sha256.Sum256(data)
			fmt.Fprintf(h, "gowork_sum=%s\n", hex.EncodeToString(sum[:8]))
		}
		gw := parseGoWork(goworkPath)
		// Replacement-only modules are build inputs too, including transitive
		// dependencies that are not selected directly by an @require directive.
		dirs := append([]string(nil), gw.useDirs...)
		dirs = append(dirs, gw.localReplaceDirs...)
		sort.Strings(dirs)
		for _, dir := range slices.Compact(dirs) {
			fmt.Fprintf(h, "gowork_mod=%s:%s\n", dir, moduleSourceFingerprint(dir))
		}
	}

	if root, err := selfModuleRoot(); err == nil {
		fmt.Fprintf(h, "source=%s\n", moduleSourceFingerprint(root))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// moduleSourceFingerprint hashes local build inputs by content, not mtimes.
// Include non-Go files because go:embed may consume NFlow, YAML, text or other
// assets. Extra invalidations are safer than reusing a stale compiled runner.
func moduleSourceFingerprint(root string) string {
	h := sha256.New()
	fmt.Fprintln(h, "module-content-v2")
	walkErr := func() error {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return err
		}
		confined, err := xfs.OpenRoot(canonical)
		if err != nil {
			return err
		}
		defer func() { _ = confined.Close() }()
		cacheRoot, err := filepath.Abs(harnessCacheRoot())
		if err != nil {
			return err
		}
		if resolved, resolveErr := filepath.EvalSymlinks(cacheRoot); resolveErr == nil {
			cacheRoot = resolved
		}
		return filepath.WalkDir(canonical, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				// Cached runners are outputs, not inputs, even inside a source module.
				if path == cacheRoot && path != canonical {
					return fs.SkipDir
				}
				switch d.Name() {
				case ".git", "node_modules":
					return fs.SkipDir
				}
				return nil
			}
			// Lock files are outputs of dependency resolution, not module inputs.
			if strings.HasSuffix(d.Name(), ".nflow.lock") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				// Go source discovery and go:embed do not follow file symlinks.
				return nil
			}
			rel, err := filepath.Rel(canonical, path)
			if err != nil {
				return err
			}
			file, err := confined.Open(rel)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "file=%q size=%d\n", filepath.ToSlash(rel), info.Size())
			_, copyErr := io.Copy(h, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			fmt.Fprintln(h)
			return nil
		})
	}()
	if walkErr != nil {
		slog.Debug("harness: source fingerprint walk", "err", walkErr)
		// A failed scan must not reuse an earlier partial fingerprint.
		fmt.Fprintf(h, "unreadable=%s\n", rand.Text())
	}
	return hex.EncodeToString(h.Sum(nil))
}

const harnessCacheEnv = "NFLOW_HARNESS_CACHE"

func harnessCacheRoot() string {
	base := os.Getenv(harnessCacheEnv)
	if base != "" {
		return base
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "nflow", "harness")
}

func harnessCacheDir(key string) (string, error) {
	dir := filepath.Join(harnessCacheRoot(), key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("harness: cache dir: %w", err)
	}
	return dir, nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func runGoContext(parent context.Context, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, goCommandTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin

	capture := newTailBuffer(maxCapturedOutputBytes)
	if os.Getenv("NFLOW_GO_VERBOSE") == "1" {
		cmd.Stdout = io.MultiWriter(os.Stderr, capture)
		cmd.Stderr = io.MultiWriter(os.Stderr, capture)
	} else {
		cmd.Stdout = capture
		cmd.Stderr = capture
	}

	cmd.Env = append(os.Environ(),
		"GOWORK=off",
		"GIT_TERMINAL_PROMPT=0",
	)

	if err := cmd.Run(); err != nil {
		return goCommandFailure(ctx, args, err, capture.String())
	}
	return nil
}

func runGoOutputContext(parent context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, goCommandTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	var stdout bytes.Buffer
	capture := newTailBuffer(maxCapturedOutputBytes)
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, capture)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		return nil, goCommandFailure(ctx, args, err, capture.String())
	}
	return stdout.Bytes(), nil
}

func goCommandFailure(ctx context.Context, args []string, err error, output string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("go %s: timed out after %s: %w", strings.Join(args, " "), goCommandTimeout(), ctx.Err())
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, output)
}

func resolveRequirementPackages(reqs []require.Requirement, driverRoot, driverVersion, goworkPath string, sourceDirs ...string) ([]require.Requirement, error) {
	return resolveRequirementPackagesWithContext(context.Background(), reqs, driverRoot, driverVersion, goworkPath, sourceDirs...)
}

func resolveRequirementPackagesWithContext(ctx context.Context, reqs []require.Requirement, driverRoot, driverVersion, goworkPath string, sourceDirs ...string) ([]require.Requirement, error) {
	sourceDir := ""
	if len(sourceDirs) > 0 {
		sourceDir = sourceDirs[0]
	}
	resolved := append([]require.Requirement(nil), reqs...)
	for i := range resolved {
		r := &resolved[i]
		if _, ok := compiler.Lookup(require.NormalizeID(r.Import)); ok {
			continue
		}
		if _, ok := compiler.Lookup(r.Import); ok {
			continue
		}
		switch {
		case r.IsLoose:
			r.PackagePath = bundleImportPath(*r)
		case r.IsLocal():
			if err := resolveLocalBundlePackage(r); err != nil {
				return nil, err
			}
		case r.Version == "":
			packagePath := bundleImportPath(*r)
			providerPath := unversionedPackageProvider(packagePath, driverRoot, goworkPath, sourceDir)
			if providerPath == "" {
				return nil, fmt.Errorf("@require %s: remote packages outside the current module or Go workspace need an explicit version", r.Import)
			}
			r.PackagePath = packagePath
			r.ModulePath = providerPath
		default:
			packagePath := bundleImportPath(*r)
			providerPath, providerVersion, err := resolveRemotePackageProvider(
				ctx, packagePath, r.Version, driverRoot, driverVersion, goworkPath,
			)
			if err != nil {
				return nil, fmt.Errorf("@require %s: resolve bundle package %s@%s: %w", r.Import, packagePath, r.Version, err)
			}
			r.PackagePath = packagePath
			r.ModulePath = providerPath
			r.ResolvedVersion = providerVersion
		}
	}
	return resolved, nil
}

func unversionedPackageProvider(packagePath, driverRoot, goworkPath, sourceDir string) string {
	var providers []string
	add := func(modulePath string) {
		if modulePath != "" && (packagePath == modulePath || strings.HasPrefix(packagePath, modulePath+"/")) {
			providers = append(providers, modulePath)
		}
	}
	if driverRoot != "" {
		if modulePath, err := require.ReadModuleLine(filepath.Join(driverRoot, "go.mod")); err == nil {
			add(modulePath)
		}
	}
	for modulePath := range parseGoWork(goworkPath).replaces {
		add(modulePath)
	}
	if sourceDir != "" {
		if abs, err := filepath.Abs(sourceDir); err == nil {
			if root := findModuleRoot(abs); root != "" {
				if modulePath, err := require.ReadModuleLine(filepath.Join(root, "go.mod")); err == nil {
					add(modulePath)
				}
			}
		}
	}
	if len(providers) == 0 {
		return ""
	}
	sort.Slice(providers, func(i, j int) bool { return len(providers[i]) > len(providers[j]) })
	return providers[0]
}

func resolveLocalBundlePackage(r *require.Requirement) error {
	r.PackagePath = bundleImportPath(*r)
	if r.IsLoose || r.ModuleRoot == "" || r.LocalPath == "" || hasAdapterFolderSuffix(r.Import) {
		return nil
	}
	if filepath.Clean(r.LocalPath) != filepath.Clean(r.ModuleRoot) {
		return nil
	}

	adapterDir := filepath.Join(r.LocalPath, defaultAdapterDirectory)
	if !require.HasGoFiles(adapterDir) {
		return nil
	}
	goModPath := filepath.Join(adapterDir, "go.mod")
	if info, err := os.Stat(goModPath); err == nil && !info.IsDir() {
		modulePath, err := require.ReadModuleLine(goModPath)
		if err != nil {
			return fmt.Errorf("@require %s: read nested adapter module: %w", r.Import, err)
		}
		r.ModuleRoot = adapterDir
		r.ModulePath = modulePath
		r.PackagePath = modulePath
		return nil
	}
	r.PackagePath = strings.TrimSuffix(r.Import, "/") + "/" + defaultAdapterDirectory
	return nil
}

func resolveRemotePackageProvider(ctx context.Context, packagePath, version, driverRoot, driverVersion, goworkPath string) (providerPath, providerVersion string, resolveErr error) {
	if packagePath == "" || version == "" {
		return "", "", errors.New("package path and explicit version are required")
	}
	dir, createErr := os.MkdirTemp("", "nflow-resolve-")
	if createErr != nil {
		return "", "", fmt.Errorf("create resolver context: %w", createErr)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	mod := harnessGoMod(driverRoot, driverVersion, goworkPath, nil)
	if writeErr := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); writeErr != nil {
		return "", "", fmt.Errorf("write resolver go.mod: %w", writeErr)
	}
	if _, getErr := runGoOutputContext(ctx, dir, "get", packagePath+"@"+version); getErr != nil {
		return "", "", getErr
	}
	data, listErr := runGoOutputContext(ctx, dir, "list", "-json", packagePath)
	if listErr != nil {
		return "", "", listErr
	}
	var pkg struct {
		Module *struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"Module"`
	}
	if decodeErr := json.Unmarshal(data, &pkg); decodeErr != nil {
		return "", "", fmt.Errorf("decode go list package metadata: %w", decodeErr)
	}
	if pkg.Module == nil || pkg.Module.Path == "" {
		return "", "", errors.New("go list did not report a providing module")
	}
	return pkg.Module.Path, pkg.Module.Version, nil
}

func goCommandTimeout() time.Duration {
	if raw := os.Getenv("NFLOW_GO_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
		slog.Warn("harness: ignoring invalid NFLOW_GO_TIMEOUT", "value", raw)
	}
	return defaultGoCommandTimeout
}

type tailBuffer struct {
	buf     []byte
	max     int
	dropped bool
}

func newTailBuffer(maxBytes int) *tailBuffer {
	return &tailBuffer{max: maxBytes}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	if len(p) >= b.max {
		b.buf = append(b.buf[:0], p[len(p)-b.max:]...)
		b.dropped = true
		return len(p), nil
	}
	if over := len(b.buf) + len(p) - b.max; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
		b.dropped = true
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *tailBuffer) String() string {
	if !b.dropped {
		return string(b.buf)
	}
	return "… (earlier output truncated)\n" + string(b.buf)
}

type bundleImport struct {
	alias    string
	path     string
	target   string
	options  string
	reqAlias string
}

func prepareBundleImports(directory string, requirements []require.Requirement) ([]bundleImport, error) {
	seen := make(map[string]bool)
	imports := make([]bundleImport, 0, len(requirements))
	for i := range requirements {
		r := &requirements[i]
		importPath := bundleImportPath(*r)
		if importPath == "" || seen[importPath] {
			continue
		}
		seen[importPath] = true

		if r.IsLoose {
			targetDir := filepath.Join(directory, "loose", r.LooseID)
			if err := copyLoosePackage(*r, targetDir); err != nil {
				return nil, fmt.Errorf("copy loose package %s: %w", r.LocalPath, err)
			}
		}

		imports = append(imports, bundleImport{
			alias:    fmt.Sprintf("nflowbundle%d", i),
			path:     importPath,
			target:   r.Import,
			options:  optionsLiteral(r.Options),
			reqAlias: r.Alias,
		})
	}
	return imports, nil
}

func writeBundleImports(mainBuilder *strings.Builder, imports []bundleImport) {
	for i := range imports {
		item := &imports[i]
		fmt.Fprintf(mainBuilder, "\t%s %q\n", item.alias, item.path)
	}
}

func writeBundleConstruction(mainBuilder *strings.Builder, imports []bundleImport) {
	for i := range imports {
		item := &imports[i]
		mainBuilder.WriteString("\t{\n")
		fmt.Fprintf(mainBuilder, "\t\tb := %s.Bundle(%s)\n", item.alias, item.options)
		if item.reqAlias != "" {
			fmt.Fprintf(mainBuilder, "\t\tb.Alias = %q\n", item.reqAlias)
		}
		mainBuilder.WriteString("\t\tif err := adopt(b); err != nil { return err }\n")
		mainBuilder.WriteString("\t}\n")
	}
}

func writeBundleTargets(mainBuilder *strings.Builder, imports []bundleImport) {
	mainBuilder.WriteString("\ttargets := []string{\n")
	for i := range imports {
		fmt.Fprintf(mainBuilder, "\t\t%q,\n", imports[i].target)
	}
	mainBuilder.WriteString("\t}\n")
}

func writeHarness(
	directory, driverRoot, driverVersion, goworkPath string,
	usedNative []nativeBundleInfo,
	requirements []require.Requirement,
	sourceDirs ...string,
) error {
	imports, err := prepareBundleImports(directory, requirements)
	if err != nil {
		return fmt.Errorf("harness: %w", err)
	}

	var mainBuilder strings.Builder
	mainBuilder.WriteString("package main\n\n")
	mainBuilder.WriteString("import (\n")
	mainBuilder.WriteString("\t\"os\"\n\n")
	mainBuilder.WriteString("\t\"github.com/nexssp/flow/cli\"\n")
	mainBuilder.WriteString("\tcompiler \"github.com/nexssp/flow/compiler\"\n")

	for i := range usedNative {
		fmt.Fprintf(&mainBuilder, "\tnative%d %q\n", i, usedNative[i].pkg)
	}

	writeBundleImports(&mainBuilder, imports)

	mainBuilder.WriteString(")\n\n")
	mainBuilder.WriteString("func main() {\n")

	writeBundleTargets(&mainBuilder, imports)

	mainBuilder.WriteString("\tos.Exit(cli.RunWithBundleFactoriesAndNative(\n")
	mainBuilder.WriteString("\t\tos.Args[1:],\n")
	mainBuilder.WriteString("\t\ttargets,\n")
	mainBuilder.WriteString("\t\tfunc() []compiler.Bundle {\n")
	mainBuilder.WriteString("\t\t\treturn []compiler.Bundle{\n")

	for i := range usedNative {
		fmt.Fprintf(&mainBuilder, "\t\t\t\tnative%d.Bundle(nil),\n", i)
	}

	mainBuilder.WriteString("\t\t\t}\n")
	mainBuilder.WriteString("\t\t},\n")
	mainBuilder.WriteString("\t\tfunc(adopt func(compiler.Bundle) error) error {\n")

	writeBundleConstruction(&mainBuilder, imports)

	mainBuilder.WriteString("\t\t\treturn nil\n")
	mainBuilder.WriteString("\t\t},\n")
	mainBuilder.WriteString("\t))\n")
	mainBuilder.WriteString("}\n")

	if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte(mainBuilder.String()), 0o600); err != nil {
		return fmt.Errorf("harness: write main.go: %w", err)
	}

	moduleDefinition := harnessGoMod(driverRoot, driverVersion, goworkPath, requirements, sourceDirs...)
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(moduleDefinition), 0o600); err != nil {
		return fmt.Errorf("harness: write go.mod: %w", err)
	}
	return nil
}

func copyLoosePackage(r require.Requirement, dstDir string) error {
	srcDir := r.LocalPath
	candidate := filepath.Join(srcDir, defaultAdapterDirectory)
	if require.HasGoFiles(candidate) {
		info, err := os.Lstat(candidate)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("harness: loose-package adapter directory %q must not be a symlink", candidate)
		}
		srcDir = candidate
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("harness: loose-package Go source %q must not be a symlink", entry.Name())
		}
		srcFile := filepath.Join(srcDir, entry.Name())
		data, err := os.ReadFile(srcFile)
		if err != nil {
			return err
		}

		if isPackageMain(data) {
			pkgName := filepath.Base(dstDir)
			return fmt.Errorf(
				"file %q declares `package main`.\n"+
					"  Loose @require packages must be libraries. Change it to `package %s`",
				srcFile, pkgName)
		}

		name := entry.Name()
		if !filepath.IsLocal(name) || filepath.Base(name) != name {
			return fmt.Errorf("harness: invalid loose-package filename %q", name)
		}
		dstFile := filepath.Join(dstDir, name)
		//nolint:gosec // name is validated as one local path component inside the temporary destination.
		if err := os.WriteFile(dstFile, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func isPackageMain(data []byte) bool {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") {
			continue
		}
		if pkg, ok := strings.CutPrefix(line, "package "); ok {
			return strings.TrimSpace(pkg) == "main"
		}
	}
	return false
}

func bundleImportPath(r require.Requirement) string {
	if r.PackagePath != "" {
		return r.PackagePath
	}
	if r.IsLoose {
		return r.Import
	}
	base := strings.TrimSuffix(r.Import, "/")
	if base == "" {
		return ""
	}

	if hasAdapterFolderSuffix(base) {
		return base
	}

	if r.IsLocal() {
		if !require.HasGoFiles(filepath.Join(r.LocalPath, defaultAdapterDirectory)) && require.HasGoFiles(r.LocalPath) {
			return base
		}
	}

	return base + "/" + defaultAdapterDirectory
}

func hasAdapterFolderSuffix(importPath string) bool {
	clean := strings.TrimSuffix(strings.ReplaceAll(importPath, `\`, "/"), "/")
	segment := clean
	if slash := strings.LastIndexByte(clean, '/'); slash >= 0 {
		segment = clean[slash+1:]
	}
	return segment == defaultAdapterDirectory || strings.HasPrefix(segment, defaultAdapterDirectory+"_")
}

func explainBuildError(err error, requirements []require.Requirement) error {
	if err == nil {
		return nil
	}
	message := err.Error()

	if req := offendingBundleRequirement(message, requirements); req != nil {
		return missingBundleSymbolError(*req)
	}

	for i := range requirements {
		r := &requirements[i]
		importPath := bundleImportPath(*r)
		if importPath == "" || !strings.Contains(message, importPath) {
			continue
		}
		if r.IsLoose {
			return fmt.Errorf(
				"@require %s: compilation failed for loose package %q:\n%s\n\n"+
					"  💡 Check that your .go files:\n"+
					"     1. Declare `func Bundle(opts map[string]string) compiler.Bundle`\n"+
					"     2. Do not declare `package main` (use `package %s` instead)",
				r.LocalPath, r.LocalPath, message, tailSegment(r.LocalPath))
		}
		if strings.Contains(message, "cannot find package") || strings.Contains(message, "no Go files in") {
			return fmt.Errorf(
				"@require %s: no Nexss Flow bundle found at %q.\n\n"+
					"Every extension must provide its Bundle function:\n"+
					"    package nexssflow\n\n"+
					"    import (\n"+
					"        compiler \"github.com/nexssp/flow/compiler\"\n"+
					"        \"github.com/nexssp/kernel/action\"\n"+
					"    )\n\n"+
					"    const ID = %q\n\n"+
					"    func init() { compiler.Register(ID, Bundle) }\n\n"+
					"    func Bundle(_ map[string]string) compiler.Bundle {\n"+
					"        return compiler.Bundle{\n"+
					"            ID:        ID,\n"+
					"            Libraries: []action.Library{{Name: ID}},\n"+
					"        }\n"+
					"    }\n\n"+
					"  💡 Run `nflow init --bundle` in that directory to scaffold it.\n\n"+
					"details: %s",
				r.Import, importPath, tailSegment(r.Import), message)
		}
		return fmt.Errorf("@require %s: build error:\n%s", r.Import, message)
	}
	return err
}

func offendingBundleRequirement(message string, requirements []require.Requirement) *require.Requirement {
	const marker = "undefined: nflowbundle"
	_, after, ok := strings.Cut(message, marker)
	if !ok {
		return nil
	}
	rest := after
	dot := strings.IndexAny(rest, ".\n \t")
	if dot <= 0 {
		return nil
	}
	n, convErr := strconv.Atoi(rest[:dot])
	if convErr != nil || n < 0 || n >= len(requirements) {
		return nil
	}
	return &requirements[n]
}

func missingBundleSymbolError(r require.Requirement) error {
	resolved := bundleImportPath(r)
	alreadySuffixed := hasAdapterFolderSuffix(r.Import)

	if alreadySuffixed {
		return fmt.Errorf(
			"@require %s: package %q does not export Bundle.\n\n"+
				"Every Flow extension package must declare:\n"+
				"    func Bundle(opts map[string]string) compiler.Bundle\n\n"+
				"Check that %s contains library.go with a Bundle function",
			r.Import, resolved, resolved)
	}

	suggestion := r.Import + "/" + defaultAdapterDirectory

	return fmt.Errorf(
		"@require %s: package %q does not export Bundle.\n\n"+
			"The resolver appended the conventional adapter suffix:\n"+
			"    requested:  %s\n"+
			"    resolved:   %s\n\n"+
			"Check that the adapter package %q exists and declares:\n"+
			"    func Bundle(opts map[string]string) compiler.Bundle\n\n"+
			"  💡 If this repository is a Flow extension with Bundle at its root,\n"+
			"     not under /nexssflow, verify the module path in its go.mod",
		r.Import, resolved, r.Import, resolved, suggestion)
}

func tailSegment(path string) string {
	path = strings.TrimSuffix(path, "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func optionsLiteral(opts map[string]string) string {
	if len(opts) == 0 {
		return "nil"
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("map[string]string{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q: %q", k, opts[k])
	}
	b.WriteString("}")
	return b.String()
}

func harnessGoMod(driverRoot, driverVersion, goworkPath string, reqs []require.Requirement, sourceDirs ...string) string {
	var b strings.Builder
	b.WriteString("module nflow-harness\n\n")

	goVersion := "go 1.23"
	if driverRoot != "" {
		if driverMod, err := os.ReadFile(filepath.Join(driverRoot, "go.mod")); err == nil {
			for line := range strings.SplitSeq(string(driverMod), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "go ") {
					goVersion = strings.TrimSpace(line)
					break
				}
			}
		}
	}
	b.WriteString(goVersion + "\n\n")

	writeRequires(&b, reqs, driverVersion, driverRoot)
	replacedModules := writeReplaces(&b, driverRoot, reqs)
	writeWorkspaceReplaces(&b, goworkPath, replacedModules)
	if len(sourceDirs) > 0 {
		writeCurrentModuleReplace(&b, sourceDirs[0], driverRoot, goworkPath, reqs)
	}

	return b.String()
}

func writeRequires(b *strings.Builder, reqs []require.Requirement, driverVersion, driverRoot string) {
	flowVersion := driverVersion
	if flowVersion == "" || flowVersion == "dev" {
		flowVersion = "v0.0.0"
	}

	versions := driverModuleVersions(flowVersion, driverRoot)
	for i := range reqs {
		addRequiredModuleVersion(versions, &reqs[i])
	}
	writeRequiredModules(b, versions)
}

func driverModuleVersions(flowVersion, driverRoot string) map[string]string {
	versions := map[string]string{"github.com/nexssp/flow": flowVersion}
	if driverRoot != "" {
		if dependencies, err := readModuleRequirements(filepath.Join(driverRoot, "go.mod")); err == nil {
			for _, dependency := range dependencies {
				if dependency.path != "github.com/nexssp/flow" {
					versions[dependency.path] = dependency.version
				}
			}
		}
	}
	return versions
}

func addRequiredModuleVersion(versions map[string]string, r *require.Requirement) {
	if r.IsLoose {
		return
	}
	targetID := require.NormalizeID(r.Import)
	if _, ok := compiler.Lookup(targetID); ok {
		return
	}
	if _, ok := compiler.Lookup(r.Import); ok {
		return
	}
	mod := modulePathForRequirement(*r)
	if mod == "" || mod == "github.com/nexssp/flow" || mod == "nflow-harness" {
		return
	}
	firstSegment, _, _ := strings.Cut(mod, "/")
	if !strings.Contains(firstSegment, ".") {
		return
	}
	version := r.ResolvedVersion
	if version == "" {
		version = r.Version
	}
	if version == "" {
		version = "v0.0.0"
	}
	if selected := versions[mod]; selected != "" && semver.Compare(selected, version) >= 0 {
		return
	}
	versions[mod] = version
}

func writeRequiredModules(b *strings.Builder, versions map[string]string) {
	modules := make([]string, 0, len(versions))
	for module := range versions {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	b.WriteString("require (\n")
	for _, module := range modules {
		fmt.Fprintf(b, "\t%s %s\n", module, versions[module])
	}
	b.WriteString(")\n\n")
}

func writeReplaces(b *strings.Builder, driverRoot string, reqs []require.Requirement) map[string]bool {
	replacedModules := make(map[string]bool)

	if driverRoot != "" {
		fmt.Fprintf(b, "replace github.com/nexssp/flow => %s\n", driverRoot)
		replacedModules["github.com/nexssp/flow"] = true

		if driverMod, err := os.ReadFile(filepath.Join(driverRoot, "go.mod")); err == nil {
			for _, directive := range goModReplaceDirectives(string(driverMod)) {
				parts := strings.Fields(directive)
				if len(parts) >= 2 {
					replacedModules[parts[1]] = true
				}
				b.WriteString(absolutizeReplace(driverRoot, directive))
				b.WriteString("\n")
			}
		}
	}

	for i := range reqs {
		r := &reqs[i]
		if r.IsLoose || !r.IsLocal() || r.LocalPath == "" {
			continue
		}
		mod := modulePathForRequirement(*r)
		if !replacedModules[mod] {
			replacedModules[mod] = true
			fmt.Fprintf(b, "replace %s => %s\n", mod, r.ModuleRoot)
		}
	}
	return replacedModules
}

func writeWorkspaceReplaces(b *strings.Builder, goworkPath string, replacedModules map[string]bool) {
	replacesFromWork := parseGoWorkReplaces(goworkPath)
	if len(replacesFromWork) == 0 {
		return
	}

	workKeys := make([]string, 0, len(replacesFromWork))
	for k := range replacesFromWork {
		workKeys = append(workKeys, k)
	}
	sort.Strings(workKeys)

	for _, mod := range workKeys {
		if replacedModules[mod] {
			continue
		}
		fmt.Fprintf(b, "replace %s => %s\n", mod, replacesFromWork[mod])
	}
}

func writeCurrentModuleReplace(b *strings.Builder, sourceDir, driverRoot, goworkPath string, reqs []require.Requirement) {
	modulePath, root := currentModuleRequirement(sourceDir, reqs)
	if modulePath == "" || moduleReplacementExists(modulePath, driverRoot, goworkPath, reqs) {
		return
	}
	fmt.Fprintf(b, "replace %s => %s\n", modulePath, root)
}

func currentModuleRequirement(sourceDir string, reqs []require.Requirement) (resolvedModulePath, resolvedRoot string) {
	if sourceDir == "" {
		return "", ""
	}
	abs, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", ""
	}
	root := findModuleRoot(abs)
	if root == "" {
		return "", ""
	}
	modulePath, err := require.ReadModuleLine(filepath.Join(root, "go.mod"))
	if err != nil || modulePath == "" {
		return "", ""
	}
	for i := range reqs {
		r := &reqs[i]
		if !r.IsLocal() && !r.IsLoose && r.Version == "" && r.ModulePath == modulePath {
			return modulePath, root
		}
	}
	return "", ""
}

func moduleReplacementExists(modulePath, driverRoot, goworkPath string, reqs []require.Requirement) bool {
	if driverRoot != "" {
		if driverPath, err := require.ReadModuleLine(filepath.Join(driverRoot, "go.mod")); err == nil && driverPath == modulePath {
			return true
		}
		if driverModuleReplaces(driverRoot, modulePath) {
			return true
		}
	}
	for i := range reqs {
		r := &reqs[i]
		if r.IsLocal() && modulePathForRequirement(*r) == modulePath {
			return true
		}
	}
	_, exists := parseGoWork(goworkPath).replaces[modulePath]
	return exists
}

func driverModuleReplaces(driverRoot, modulePath string) bool {
	data, err := os.ReadFile(filepath.Join(driverRoot, "go.mod"))
	if err != nil {
		return false
	}
	for _, directive := range goModReplaceDirectives(string(data)) {
		parts := strings.Fields(directive)
		if len(parts) >= 2 && parts[1] == modulePath {
			return true
		}
	}
	return false
}

type goWorkInfo struct {
	useDirs          []string
	replaces         map[string]string
	localReplaceDirs []string
}

func parseGoWork(goworkPath string) goWorkInfo {
	info := goWorkInfo{replaces: make(map[string]string)}
	if goworkPath == "" {
		return info
	}

	file, err := os.Open(goworkPath)
	if err != nil {
		return info
	}
	defer func() { _ = file.Close() }()

	baseDir := filepath.Dir(goworkPath)
	scanner := bufio.NewScanner(file)

	inUse := false
	inReplace := false

	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if line == ")" {
			inUse = false
			inReplace = false
			continue
		}

		switch {
		case line == "use (" || strings.HasPrefix(line, "use ("):
			inUse = true
			continue
		case line == "replace (" || strings.HasPrefix(line, "replace ("):
			inReplace = true
			continue
		case strings.HasPrefix(line, "use "):
			rawDir := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "use")), `"'`)
			if rawDir != "" {
				info.useDirs = append(info.useDirs, absolutizeDir(baseDir, rawDir))
			}
			continue
		case strings.HasPrefix(line, "replace "):
			parseReplaceLine(baseDir, strings.TrimPrefix(line, "replace"), &info)
			continue
		}

		if inUse {
			rawDir := strings.Trim(line, `"'`)
			if rawDir != "" {
				info.useDirs = append(info.useDirs, absolutizeDir(baseDir, rawDir))
			}
		} else if inReplace {
			parseReplaceLine(baseDir, line, &info)
		}
	}

	for _, dir := range info.useDirs {
		goModPath := filepath.Join(dir, "go.mod")
		if modName, err := require.ReadModuleLine(goModPath); err == nil && modName != "" {
			info.replaces[modName] = dir
		}
	}

	return info
}

func parseReplaceLine(baseDir, line string, info *goWorkInfo) {
	parts := strings.Fields(line)
	var oldMod, newTarget string
	switch {
	case len(parts) == 3 && parts[1] == "=>":
		oldMod = strings.Trim(parts[0], `"'`)
		newTarget = strings.Trim(parts[2], `"'`)
	case len(parts) == 4 && parts[2] == "=>":
		oldMod = strings.Trim(parts[0], `"'`)
		newTarget = strings.Trim(parts[3], `"'`)
	case len(parts) == 5 && parts[2] == "=>":
		oldMod = strings.Trim(parts[0], `"'`)
		newTarget = strings.Trim(parts[4], `"'`)
	}

	if oldMod != "" && newTarget != "" {
		target := absolutizeDir(baseDir, newTarget)
		info.replaces[oldMod] = target
		// Preserve replacement resolution, but only fingerprint local sources.
		if modfile.IsDirectoryPath(newTarget) {
			info.localReplaceDirs = append(info.localReplaceDirs, target)
		}
	}
}

func absolutizeDir(base, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

func parseGoWorkReplaces(goworkPath string) map[string]string {
	if goworkPath == "" {
		return nil
	}
	gw := parseGoWork(goworkPath)
	return gw.replaces
}

func findGoWork(start string) string {
	if os.Getenv("GOWORK") == "off" {
		return ""
	}
	if explicit := os.Getenv("GOWORK"); explicit != "" && explicit != "auto" {
		//nolint:gosec // GOWORK is an explicit user-selected file path and is only inspected.
		if _, err := os.Stat(explicit); err == nil {
			return explicit
		}
	}

	curr := start
	if info, err := os.Stat(curr); err == nil && !info.IsDir() {
		curr = filepath.Dir(curr)
	}
	for {
		candidate := filepath.Join(curr, "go.work")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return ""
		}
		curr = parent
	}
}

func moduleRootOf(importPath string) string {
	importPath = strings.TrimSuffix(importPath, "/")
	if importPath == "" {
		return ""
	}
	parts := strings.Split(importPath, "/")
	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		return strings.Join(parts[:3], "/")
	}
	if len(parts) >= 2 {
		return strings.Join(parts[:2], "/")
	}
	return parts[0]
}

func modulePathForRequirement(r require.Requirement) string {
	if r.ModulePath != "" {
		return r.ModulePath
	}
	if modulePath := flowModulePathForPackage(r.Import); modulePath != "" {
		return modulePath
	}
	if !r.IsLocal() && r.Version != "" {
		return r.Import
	}
	return moduleRootOf(r.Import)
}

const flowRootModulePath = "github.com/nexssp/flow"

func flowModulePathForPackage(importPath string) string {
	if importPath == flowRootModulePath || strings.HasPrefix(importPath, flowRootModulePath+"/") {
		return flowRootModulePath
	}
	return ""
}

func absolutizeReplace(dir, line string) string {
	parts := strings.Fields(line)
	switch {
	case len(parts) == 4 && parts[2] == "=>":
		if isRelPath(parts[3]) {
			parts[3] = filepath.Join(dir, parts[3])
		}
	case len(parts) == 5 && parts[2] == "=>":
		if isRelPath(parts[4]) {
			parts[4] = filepath.Join(dir, parts[4])
		}
	}
	return strings.Join(parts, " ")
}

func goModReplaceDirectives(data string) []string {
	var directives []string
	inBlock := false
	for line := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if inBlock {
			if trimmed == ")" {
				inBlock = false
				continue
			}
			directives = append(directives, "replace "+trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "replace (") {
			inBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "replace ") {
			directives = append(directives, trimmed)
		}
	}
	return directives
}

func isRelPath(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
}

func resolveDriverInfo() (root, version string) {
	if r, err := selfModuleRoot(); err == nil {
		return r, ""
	}
	if Version != "" && Version != "dev" {
		return "", Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return "", bi.Main.Version
		}
	}
	return "", "v0.0.0"
}

func selfModuleRoot() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if root := walkForModule(exe); root != "" {
			return root, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if root := walkForModule(wd); root != "" {
			return root, nil
		}
	}
	return "", errors.New("harness: nexssp/flow module root not found")
}

func walkForModule(start string) string {
	current := start
	if info, err := os.Stat(current); err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		data, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/nexssp/flow") {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}
