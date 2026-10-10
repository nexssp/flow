package cli

import (
	"bufio"
	"bytes"
	"context"
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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
)

const defaultGoCommandTimeout = 10 * time.Minute

const defaultAdapterDirectory = "nexssflow"

const maxCapturedOutputBytes = 8 * 1024

const harnessLockStaleAfter = 5 * time.Minute

func EnsureHarness(reqs []require.Requirement, flowPath ...string) (string, error) {
	return EnsureHarnessContext(context.Background(), reqs, flowPath...)
}

func EnsureHarnessContext(ctx context.Context, reqs []require.Requirement, flowPath ...string) (string, error) {
	external := filterExternalRequires(reqs)
	if len(external) == 0 {
		return "", errors.New("harness: no external modules to link")
	}
	if len(flowPath) == 0 {
		return "", errors.New("harness: at least one flow file is required")
	}

	flowFile := flowPath[0]
	flowDir := flowDirectory(flowFile)

	// Concatenate every source passed in. The native-bundle detector below
	// (and inside writeHarness) scans for tokens such as "@pipeline" and
	// "@schema"; reading only flowPath[0] would miss extensions used solely
	// by siblings in a batch like `nflow lint ./examples/...`, producing a
	// harness that cannot preprocess those files. The union also seeds the
	// cache key, so single-file and batch invocations cannot collide on a
	// harness that was tree-shaken for a different token set.
	var sourceBuilder strings.Builder
	for i, p := range flowPath {
		data, readErr := readSourceFile(p)
		if readErr != nil {
			if i == 0 {
				return "", fmt.Errorf("harness: %w", readErr)
			}
			// Non-anchor files in a batch may disappear between discovery
			// and harness build; skip them rather than fail the batch.
			continue
		}
		if sourceBuilder.Len() > 0 {
			sourceBuilder.WriteByte('\n')
		}
		sourceBuilder.Write(data)
	}
	source := sourceBuilder.String()

	usedNative := detectUsedNativeBundles(source, false)
	nativeIDs := make([]string, len(usedNative))
	for i := range usedNative {
		nativeIDs[i] = usedNative[i].id
	}

	goworkPath := findGoWork(flowDir)
	driverRoot, driverVersion := resolveDriverInfo()

	resolved, err := resolveRequirementPackagesWithContext(ctx, external, driverRoot, driverVersion, goworkPath, flowDir)
	if err != nil {
		return "", err
	}
	external = resolved

	key := harnessKey(external, goworkPath, nativeIDs)

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

	buildDir, err := os.MkdirTemp("", "nflow-harness-")
	if err != nil {
		return "", fmt.Errorf("harness: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	if err := writeHarness(buildDir, driverRoot, driverVersion, goworkPath, source, external, flowDir); err != nil {
		return "", err
	}

	tidyStep := startProgress("⚙️  nflow: resolving dependencies (go mod tidy -e)")
	tidyErr := runGoContext(ctx, buildDir, "mod", "tidy", "-e")
	if tidyErr != nil {
		tidyStep.Complete("failed")
		return "", explainBuildError(tidyErr, external)
	}
	tidyStep.Complete("done")

	tmp := bin + ".tmp." + strconv.Itoa(os.Getpid())
	buildStep := startProgress(fmt.Sprintf("🔨 nflow: compiling runner binary [%s]", key[:8]))
	buildErr := runGoContext(ctx, buildDir, "build", "-trimpath", "-o", tmp, ".")
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
		if _, ok := core.Lookup(targetID); ok {
			continue
		}
		if _, ok := core.Lookup(r.Import); ok {
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

func harnessKey(reqs []require.Requirement, goworkPath string, nativeBundleIDs []string) string {
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

	// The detected native extension set is part of the harness identity:
	// two files with identical @require lists but different @pipeline /
	// @schema / @hook usage must not share a cached tree-shaken binary.
	ids := append([]string(nil), nativeBundleIDs...)
	sort.Strings(ids)
	fmt.Fprintf(h, "native=%s\n", strings.Join(ids, ","))
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
			fmt.Fprintf(h, "  fingerprint=%s\n", moduleSourceFingerprint(r.LocalPath))
		}
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
		sort.Strings(gw.useDirs)
		for _, dir := range gw.useDirs {
			fmt.Fprintf(h, "gowork_mod=%s:%s\n", dir, moduleSourceFingerprint(dir))
		}
	}

	if root, err := selfModuleRoot(); err == nil {
		fmt.Fprintf(h, "source=%s\n", moduleSourceFingerprint(root))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func moduleSourceFingerprint(root string) string {
	h := sha256.New()
	var files int
	var newest int64

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		files++
		if mtime := info.ModTime().UnixNano(); mtime > newest {
			newest = mtime
		}
		return nil
	})
	if walkErr != nil {
		slog.Debug("harness: source fingerprint walk", "err", walkErr)
	}

	fmt.Fprintf(h, "files=%d newest=%d", files, newest)
	return hex.EncodeToString(h.Sum(nil))[:8]
}

const harnessCacheEnv = "NFLOW_HARNESS_CACHE"

func harnessCacheDir(key string) (string, error) {
	base := os.Getenv(harnessCacheEnv)
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		base = filepath.Join(base, "nflow", "harness")
	}
	dir := filepath.Join(base, key)
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
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("go %s: timed out after %s", strings.Join(args, " "), goCommandTimeout())
		}
		return fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, capture.String())
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
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("go %s: timed out after %s", strings.Join(args, " "), goCommandTimeout())
		}
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, capture.String())
	}
	return stdout.Bytes(), nil
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
		if _, ok := core.Lookup(require.NormalizeID(r.Import)); ok {
			continue
		}
		if _, ok := core.Lookup(r.Import); ok {
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
	directory, driverRoot, driverVersion, goworkPath, source string,
	requirements []require.Requirement,
	sourceDirs ...string,
) error {
	imports, err := prepareBundleImports(directory, requirements)
	if err != nil {
		return fmt.Errorf("harness: %w", err)
	}

	usedNative := detectUsedNativeBundles(source, false)

	var mainBuilder strings.Builder
	mainBuilder.WriteString("package main\n\n")
	mainBuilder.WriteString("import (\n")
	mainBuilder.WriteString("\t\"os\"\n\n")
	mainBuilder.WriteString("\t\"github.com/nexssp/flow/cli\"\n")
	mainBuilder.WriteString("\t\"github.com/nexssp/flow/core\"\n")

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
	mainBuilder.WriteString("\t\tfunc() []core.Bundle {\n")
	mainBuilder.WriteString("\t\t\treturn []core.Bundle{\n")

	for i := range usedNative {
		fmt.Fprintf(&mainBuilder, "\t\t\t\tnative%d.Bundle(nil),\n", i)
	}

	mainBuilder.WriteString("\t\t\t}\n")
	mainBuilder.WriteString("\t\t},\n")
	mainBuilder.WriteString("\t\tfunc(adopt func(core.Bundle) error) error {\n")

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

		dstFile := filepath.Join(dstDir, entry.Name())
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
					"     1. Declare `func Bundle(opts map[string]string) core.Bundle`\n"+
					"     2. Do not declare `package main` (use `package %s` instead)",
				r.LocalPath, r.LocalPath, message, tailSegment(r.LocalPath))
		}
		if strings.Contains(message, "cannot find package") || strings.Contains(message, "no Go files in") {
			return fmt.Errorf(
				"@require %s: no Nexss Flow bundle found at %q.\n\n"+
					"Every extension must provide its Bundle function:\n"+
					"    package nexssflow\n\n"+
					"    import (\n"+
					"        \"github.com/nexssp/flow/core\"\n"+
					"        \"github.com/nexssp/kernel/action\"\n"+
					"    )\n\n"+
					"    const ID = %q\n\n"+
					"    func init() { core.Register(ID, Bundle) }\n\n"+
					"    func Bundle(_ map[string]string) core.Bundle {\n"+
					"        return core.Bundle{\n"+
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
				"    func Bundle(opts map[string]string) core.Bundle\n\n"+
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
			"    func Bundle(opts map[string]string) core.Bundle\n\n"+
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

	writeRequires(&b, reqs, driverVersion)
	writeReplaces(&b, driverRoot, reqs)
	writeWorkspaceReplaces(&b, goworkPath)
	if len(sourceDirs) > 0 {
		writeCurrentModuleReplace(&b, sourceDirs[0], driverRoot, goworkPath, reqs)
	}

	return b.String()
}

func writeRequires(b *strings.Builder, reqs []require.Requirement, driverVersion string) {
	flowVersion := driverVersion
	if flowVersion == "" || flowVersion == "dev" {
		flowVersion = "v0.0.0"
	}

	b.WriteString("require (\n")
	fmt.Fprintf(b, "\tgithub.com/nexssp/flow %s\n", flowVersion)

	seen := map[string]bool{}

	for i := range reqs {
		r := &reqs[i]
		if r.IsLoose {
			continue
		}

		targetID := require.NormalizeID(r.Import)
		if _, ok := core.Lookup(targetID); ok {
			continue
		}
		if _, ok := core.Lookup(r.Import); ok {
			continue
		}

		mod := modulePathForRequirement(*r)
		if mod == "" || seen[mod] {
			continue
		}

		firstSegment, _, _ := strings.Cut(mod, "/")
		if !strings.Contains(firstSegment, ".") {
			continue
		}

		seen[mod] = true

		if mod == "github.com/nexssp/flow" || mod == "nflow-harness" {
			continue
		}

		version := r.ResolvedVersion
		if version == "" {
			version = r.Version
		}
		if version == "" {
			version = "v0.0.0"
		}
		fmt.Fprintf(b, "\t%s %s\n", mod, version)
	}
	b.WriteString(")\n\n")
}

func writeReplaces(b *strings.Builder, driverRoot string, reqs []require.Requirement) {
	replacedModules := make(map[string]bool)

	if driverRoot != "" {
		fmt.Fprintf(b, "replace github.com/nexssp/flow => %s\n", driverRoot)
		replacedModules["github.com/nexssp/flow"] = true

		if driverMod, err := os.ReadFile(filepath.Join(driverRoot, "go.mod")); err == nil {
			for line := range strings.SplitSeq(string(driverMod), "\n") {
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "replace ") {
					continue
				}
				parts := strings.Fields(trimmed)
				if len(parts) >= 2 {
					replacedModules[parts[1]] = true
				}
				b.WriteString(absolutizeReplace(driverRoot, trimmed))
				b.WriteString("\n")
			}
		}

		parentDir := filepath.Dir(driverRoot)
		if entries, err := os.ReadDir(parentDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				siblingDir := filepath.Join(parentDir, entry.Name())
				goModPath := filepath.Join(siblingDir, "go.mod")
				if modName, err := require.ReadModuleLine(goModPath); err == nil && modName != "" {
					if !replacedModules[modName] {
						replacedModules[modName] = true
						fmt.Fprintf(b, "replace %s => %s\n", modName, siblingDir)
					}
				}
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
}

func writeWorkspaceReplaces(b *strings.Builder, goworkPath string) {
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
	for line := range strings.SplitSeq(string(data), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) >= 2 && parts[0] == "replace" && parts[1] == modulePath {
			return true
		}
	}
	return false
}

type goWorkInfo struct {
	useDirs  []string
	replaces map[string]string
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
			parseReplaceLine(baseDir, strings.TrimPrefix(line, "replace"), info.replaces)
			continue
		}

		if inUse {
			rawDir := strings.Trim(line, `"'`)
			if rawDir != "" {
				info.useDirs = append(info.useDirs, absolutizeDir(baseDir, rawDir))
			}
		} else if inReplace {
			parseReplaceLine(baseDir, line, info.replaces)
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

func parseReplaceLine(baseDir, line string, dst map[string]string) {
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
		dst[oldMod] = absolutizeDir(baseDir, newTarget)
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
	if !r.IsLocal() && r.Version != "" {
		return r.Import
	}
	return moduleRootOf(r.Import)
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
