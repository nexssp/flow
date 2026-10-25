package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/nexssp/kernel/xfs"
	"golang.org/x/mod/semver"

	"github.com/nexssp/flow/compiler"
	flow_version "github.com/nexssp/flow/extensions/flow_version"
)

const (
	toolchainResolvedEnv = "NFLOW_TOOLCHAIN_RESOLVED"
	githubReleaseAPI     = "https://api.github.com/repos/nexssp/flow/releases"
	toolchainLockName    = "nflow.lock"
	windowsOS            = "windows"
	maxReleaseAssetSize  = 256 << 20
)

type githubRelease struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type toolchainResolver struct {
	apiURL   string
	cacheDir string
	client   *http.Client
	goos     string
	goarch   string
}

var (
	createToolchainResolver = newToolchainResolver
	runResolvedCoreProcess  = runResolvedCore
)

func newToolchainResolver() (*toolchainResolver, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("toolchain cache: %w", err)
	}
	return &toolchainResolver{
		apiURL:   githubReleaseAPI,
		cacheDir: filepath.Join(cache, "nflow", "toolchains"),
		client:   &http.Client{Timeout: 30 * time.Second},
		goos:     runtime.GOOS,
		goarch:   runtime.GOARCH,
	}, nil
}

func (r *toolchainResolver) listReleases(ctx context.Context) ([]githubRelease, error) {
	var releases []githubRelease
	for page := 1; page <= 20; page++ {
		endpoint := fmt.Sprintf("%s?per_page=100&page=%d", strings.TrimRight(r.apiURL, "/"), page)
		data, err := r.get(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("list official NFlow releases: %w", err)
		}
		var batch []githubRelease
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, fmt.Errorf("decode official NFlow releases: %w", err)
		}
		releases = append(releases, batch...)
		if len(batch) < 100 {
			return releases, nil
		}
	}
	return releases, nil
}

func (r *toolchainResolver) get(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid release URL %q", rawURL)
	}
	if r.isLocalHTTP() {
		base, baseErr := url.Parse(r.apiURL)
		if baseErr != nil || parsed.Host != base.Host {
			return nil, errors.New("refusing release URL outside the configured local test endpoint")
		}
	} else {
		if parsed.Scheme != "https" {
			return nil, fmt.Errorf("refusing non-HTTPS release URL %q", rawURL)
		}
		if host := parsed.Hostname(); host != "api.github.com" && host != "github.com" {
			return nil, fmt.Errorf("refusing non-GitHub release URL %q", rawURL)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "nflow-toolchain-launcher")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release server returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseAssetSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReleaseAssetSize {
		return nil, fmt.Errorf("release response exceeds %d bytes", maxReleaseAssetSize)
	}
	return data, nil
}

func (r *toolchainResolver) isLocalHTTP() bool {
	parsed, err := url.Parse(r.apiURL)
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func normalizeToolchainVersion(raw string) (string, error) {
	version := releaseVersion(strings.TrimSpace(raw))
	if version == "" || semver.Prerelease(version) != "" || semver.Build(version) != "" {
		return "", fmt.Errorf("%q is not an exact stable MAJOR.MINOR.PATCH release", raw)
	}
	return version, nil
}

func (r *toolchainResolver) resolveExact(ctx context.Context, rawVersion string) (string, error) {
	version, err := normalizeToolchainVersion(rawVersion)
	if err != nil {
		return "", err
	}
	if path, cacheErr := r.cachedBinary(version); cacheErr == nil {
		return path, nil
	}
	releases, err := r.listReleases(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot acquire pinned NFlow %s: no verified compatible cache is available while offline: %w", version, err)
	}
	release, ok := findRelease(releases, version)
	if !ok {
		return "", fmt.Errorf("official stable NFlow release %s was not found", version)
	}
	return r.installRelease(ctx, release, version)
}

func (r *toolchainResolver) resolveCompatible(ctx context.Context, constraints []string) (resolvedVersion, binaryPath string, err error) {
	if version, exact := exactConstraintVersion(constraints); exact {
		if path, cacheErr := r.cachedBinary(version); cacheErr == nil {
			return version, path, nil
		}
	}
	releases, err := r.listReleases(ctx)
	if err != nil {
		version, path, cacheErr := r.cachedCompatible(constraints)
		if cacheErr == nil {
			return version, path, nil
		}
		return "", "", fmt.Errorf("cannot resolve a compatible NFlow release: offline and no verified compatible cache is available (%w; %w)", err, cacheErr)
	}
	var candidates []string
	for i := range releases {
		version, versionErr := normalizeToolchainVersion(releases[i].TagName)
		if versionErr != nil || releases[i].Draft || releases[i].Prerelease || !matchesAllConstraints(constraints, version) {
			continue
		}
		candidates = append(candidates, version)
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("no official stable NFlow release satisfies @nflow_version %q", strings.Join(constraints, "; "))
	}
	sort.Slice(candidates, func(i, j int) bool { return semver.Compare(candidates[i], candidates[j]) > 0 })
	version := candidates[0]
	release, ok := findRelease(releases, version)
	if !ok {
		return "", "", fmt.Errorf("official NFlow release metadata for %s disappeared", version)
	}
	path, err := r.installRelease(ctx, release, version)
	return version, path, err
}

func matchesAllConstraints(constraints []string, version string) bool {
	for _, constraint := range constraints {
		ok, err := flow_version.SatisfiesConstraint(constraint, version)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

func exactConstraintVersion(constraints []string) (string, bool) {
	var exactVersion string
	for _, constraint := range constraints {
		terms := strings.Fields(constraint)
		if len(terms) != 1 {
			return "", false
		}
		versionText := terms[0]
		if after, ok := strings.CutPrefix(versionText, "=="); ok {
			versionText = after
		} else if after, ok := strings.CutPrefix(versionText, "="); ok {
			versionText = after
		} else if strings.ContainsAny(versionText, "<>") {
			return "", false
		}
		versionText = strings.TrimPrefix(versionText, "v")
		if suffix := strings.IndexAny(versionText, "-+"); suffix >= 0 {
			versionText = versionText[:suffix]
		}
		version, err := normalizeToolchainVersion(versionText)
		if err != nil || (exactVersion != "" && exactVersion != version) {
			return "", false
		}
		exactVersion = version
	}
	return exactVersion, exactVersion != ""
}

func findRelease(releases []githubRelease, version string) (githubRelease, bool) {
	for _, release := range releases {
		candidate, err := normalizeToolchainVersion(release.TagName)
		if err == nil && candidate == version && !release.Draft && !release.Prerelease {
			return release, true
		}
	}
	return githubRelease{}, false
}

func (r *toolchainResolver) cachedCompatible(constraints []string) (resolvedVersion, binaryPath string, err error) {
	entries, err := os.ReadDir(r.cacheDir)
	if err != nil {
		return "", "", err
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		version, versionErr := normalizeToolchainVersion(entry.Name())
		if versionErr != nil || !matchesAllConstraints(constraints, version) {
			continue
		}
		if _, cacheErr := r.cachedBinary(version); cacheErr == nil {
			candidates = append(candidates, version)
		}
	}
	if len(candidates) == 0 {
		return "", "", errors.New("no cached release satisfies all declared constraints")
	}
	sort.Slice(candidates, func(i, j int) bool { return semver.Compare(candidates[i], candidates[j]) > 0 })
	version := candidates[0]
	path, err := r.cachedBinary(version)
	return version, path, err
}

func (r *toolchainResolver) platformNames(version string) (archiveName, checksumName, binaryName string) {
	assetVersion := strings.TrimPrefix(version, "v")
	extension := ".tar.gz"
	binaryName = "nflow"
	if r.goos == windowsOS {
		extension = ".zip"
		binaryName = "nflow.exe"
	}
	archiveName = fmt.Sprintf("nflow_%s_%s_%s%s", assetVersion, r.goos, r.goarch, extension)
	checksumName = fmt.Sprintf("nflow_%s_checksums.txt", assetVersion)
	return archiveName, checksumName, binaryName
}

func (r *toolchainResolver) installRelease(ctx context.Context, release githubRelease, version string) (string, error) {
	archiveName, checksumName, binaryName := r.platformNames(version)
	var archiveURL, checksumURL string
	for _, asset := range release.Assets {
		switch asset.Name {
		case archiveName:
			archiveURL = asset.BrowserDownloadURL
		case checksumName:
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if archiveURL == "" || checksumURL == "" {
		return "", fmt.Errorf("release %s is missing required assets %q or %q for %s/%s", version, archiveName, checksumName, r.goos, r.goarch)
	}
	manifest, err := r.get(ctx, checksumURL)
	if err != nil {
		return "", fmt.Errorf("download checksum manifest for %s: %w", version, err)
	}
	archive, err := r.get(ctx, archiveURL)
	if err != nil {
		return "", fmt.Errorf("download official archive for %s: %w", version, err)
	}
	expected, err := checksumForAsset(manifest, archiveName)
	if err != nil {
		return "", fmt.Errorf("verify official checksum manifest: %w", err)
	}
	actual := sha256.Sum256(archive)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(expected, actualHex) {
		return "", fmt.Errorf("official checksum mismatch for %s: expected %s, got %s", archiveName, expected, actualHex)
	}
	binary, err := extractReleaseBinary(archive, r.goos, binaryName)
	if err != nil {
		return "", fmt.Errorf("extract verified NFlow %s: %w", version, err)
	}
	cacheDir := r.platformCacheDir(version)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create toolchain cache: %w", err)
	}
	archivePath := filepath.Join(cacheDir, archiveName)
	if err := atomicToolchainWrite(archivePath, archive, 0o644); err != nil {
		return "", fmt.Errorf("cache verified NFlow archive: %w", err)
	}
	if err := atomicToolchainWrite(archivePath+".sha256", []byte(actualHex+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("cache verified archive digest: %w", err)
	}
	binaryPath := filepath.Join(cacheDir, binaryName)
	mode := os.FileMode(0o755)
	if r.goos == windowsOS {
		mode = 0o644
	}
	if err := atomicToolchainWrite(binaryPath, binary, mode); err != nil {
		return "", fmt.Errorf("cache verified NFlow executable: %w", err)
	}
	binaryDigest := sha256.Sum256(binary)
	if err := atomicToolchainWrite(binaryPath+".sha256", []byte(hex.EncodeToString(binaryDigest[:])+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("cache verified NFlow executable digest: %w", err)
	}
	return binaryPath, nil
}

func (r *toolchainResolver) cachedBinary(version string) (string, error) {
	archiveName, _, binaryName := r.platformNames(version)
	cacheDir := r.platformCacheDir(version)
	binaryPath := filepath.Join(cacheDir, binaryName)
	if valid, checksumPresent := verifyCachedToolchainBinary(binaryPath); valid {
		return binaryPath, nil
	} else if !checksumPresent {
		// A legacy executable or its local archive sidecar cannot establish
		// release provenance. Force the caller through the official checksum-
		// verified install path before creating an executable digest marker.
		return "", fmt.Errorf("cached NFlow %s executable has no trusted digest; verified reinstall required", version)
	}
	archivePath := filepath.Join(cacheDir, archiveName)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		return "", err
	}
	digest, err := os.ReadFile(archivePath + ".sha256")
	if err != nil {
		return "", err
	}
	actual := sha256.Sum256(archive)
	if !strings.EqualFold(strings.TrimSpace(string(digest)), hex.EncodeToString(actual[:])) {
		return "", fmt.Errorf("cached archive checksum mismatch for %s", version)
	}
	binary, err := extractReleaseBinary(archive, r.goos, binaryName)
	if err != nil {
		return "", fmt.Errorf("cached archive for %s is invalid: %w", version, err)
	}
	mode := os.FileMode(0o755)
	if r.goos == windowsOS {
		mode = 0o644
	}
	if err := atomicToolchainWrite(binaryPath, binary, mode); err != nil {
		return "", err
	}
	binaryDigest := sha256.Sum256(binary)
	if err := atomicToolchainWrite(binaryPath+".sha256", []byte(hex.EncodeToString(binaryDigest[:])+"\n"), 0o644); err != nil {
		return "", err
	}
	return binaryPath, nil
}

func verifyCachedToolchainBinary(binaryPath string) (valid, checksumPresent bool) {
	expected, err := os.ReadFile(binaryPath + ".sha256")
	if errors.Is(err, os.ErrNotExist) {
		return false, false
	}
	if err != nil {
		return false, true
	}
	actual, err := fileSHA256(binaryPath)
	if err != nil {
		return false, true
	}
	return strings.EqualFold(strings.TrimSpace(string(expected)), actual), true
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, file)
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (r *toolchainResolver) platformCacheDir(version string) string {
	return filepath.Join(r.cacheDir, version, r.goos+"-"+r.goarch)
}

func checksumForAsset(manifest []byte, name string) (string, error) {
	var found string
	for line := range strings.SplitSeq(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		filename := strings.TrimPrefix(fields[1], "*")
		if filename != name {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("checksum manifest has duplicate entries for %q", name)
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", fmt.Errorf("checksum entry for %q is not SHA-256", name)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", fmt.Errorf("invalid checksum for %q: %w", name, err)
		}
		found = strings.ToLower(fields[0])
	}
	if found == "" {
		return "", fmt.Errorf("checksum manifest has no entry for %q", name)
	}
	return found, nil
}

func extractReleaseBinary(archive []byte, goos, binaryName string) ([]byte, error) {
	if goos == windowsOS {
		return extractZipReleaseBinary(archive, binaryName)
	}
	return extractTarGzipReleaseBinary(archive, binaryName)
}

func extractZipReleaseBinary(archive []byte, binaryName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, file := range reader.File {
		if filepath.Base(filepath.ToSlash(file.Name)) != binaryName || file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > maxReleaseAssetSize {
			return nil, errors.New("binary entry is too large")
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := readReleaseBinary(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive does not contain %s", binaryName)
}

func extractTarGzipReleaseBinary(archive []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(filepath.ToSlash(header.Name)) != binaryName || !header.FileInfo().Mode().IsRegular() {
			continue
		}
		if header.Size < 0 || header.Size > maxReleaseAssetSize {
			return nil, errors.New("binary entry is too large")
		}
		return readReleaseBinary(reader)
	}
	return nil, fmt.Errorf("archive does not contain %s", binaryName)
}

func readReleaseBinary(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxReleaseAssetSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReleaseAssetSize {
		return nil, errors.New("binary entry is too large")
	}
	return data, nil
}

func atomicToolchainWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return xfs.WriteFileAtomic(path, data, mode)
}

func currentToolchainVersion() string {
	return releaseVersion(compiler.Version)
}

func versionSatisfiesConstraints(version string, constraints []string) (bool, error) {
	for _, constraint := range constraints {
		matched, err := flow_version.SatisfiesConstraint(constraint, version)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

func collectVersionConstraints(ctx context.Context, inv *invocation, path string) ([]string, error) {
	data, err := readSourceFile(path)
	if err != nil {
		return nil, err
	}
	inspectCtx := flow_version.WithDeferredCompatibilityCheck(ctx)
	directives, err := inv.directiveTable()
	if err != nil {
		return nil, err
	}
	_, meta, err := compiler.Preprocess(inspectCtx, directives, string(data), path)
	if err != nil {
		return nil, err
	}
	var constraints []string
	var visit func(map[string]any)
	visit = func(sourceMeta map[string]any) {
		if value, ok := sourceMeta["nflow_version"].(string); ok && value != "" {
			constraints = append(constraints, value)
		}
		for _, imported := range compiler.SourceImports(sourceMeta) {
			if imported.Source != nil {
				visit(imported.Source.Metadata())
			}
		}
	}
	visit(meta)
	return constraints, nil
}

func toolchainSourceFiles(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	command := args[0]
	rest := args[1:]
	switch command {
	case "fmt":
		options, err := parseFmtOptions(rest)
		if err != nil {
			return nil, err
		}
		if isFmtStdin(options) {
			return nil, nil
		}
		files := make([]string, 0, len(options.files))
		for _, path := range options.files {
			if path != "-" {
				files = append(files, path)
			}
		}
		return files, nil
	case "lint":
		targets := make([]string, 0, len(rest))
		for _, arg := range rest {
			if !strings.HasPrefix(arg, "-") {
				targets = append(targets, arg)
			}
		}
		if len(targets) == 0 {
			return nil, nil
		}
		return discoverLintFiles(targets)
	case "lsp", versionCommand, completionCommand, toolchainCommand, "self", "init", catalogCommand, "list", "show", requireCommand, "pin":
		return nil, nil
	case "run", buildCommand, serveCommand, "info", explainCommand, expandCommand:
		target, _, _, err := splitArgs(rest)
		if err != nil {
			return nil, err
		}
		if target == "" || isInlineSource(target) {
			return nil, nil
		}
		return []string{target}, nil
	default:
		// The bare `nflow file.nflow` form is a run command.
		target, _, _, err := splitArgs(args)
		if err != nil {
			return nil, err
		}
		if target == "" || isInlineSource(target) {
			return nil, nil
		}
		return []string{target}, nil
	}
}

func nearestToolchainLock(start string) (lockVersion string, found bool, err error) {
	if start == "" || start == "-" {
		return "", false, nil
	}
	absolute, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	info, statErr := os.Stat(absolute)
	if statErr == nil && !info.IsDir() {
		absolute = filepath.Dir(absolute)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", false, statErr
	}
	for dir := absolute; ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, toolchainLockName)
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			fields := strings.Fields(string(data))
			if len(fields) != 1 {
				return "", false, fmt.Errorf("%s must contain exactly one stable release tag", path)
			}
			version, versionErr := normalizeToolchainVersion(fields[0])
			if versionErr != nil {
				return "", false, fmt.Errorf("%s: %w", path, versionErr)
			}
			return version, true, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return "", false, readErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", false, nil
}

func findToolchainLock(sourceFiles []string) (toolchainVersion string, hasLock bool, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	roots := append([]string{cwd}, sourceFiles...)
	seen := make(map[string]struct{})
	for _, root := range roots {
		candidate, found, err := nearestToolchainLock(root)
		if err != nil {
			return "", false, err
		}
		if !found {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if toolchainVersion != "" && toolchainVersion != candidate {
			return "", false, fmt.Errorf("source targets resolve to conflicting nflow.lock pins %s and %s", toolchainVersion, candidate)
		}
		toolchainVersion = candidate
	}
	return toolchainVersion, toolchainVersion != "", nil
}

func writeToolchainLock(path, version string) error {
	version, err := normalizeToolchainVersion(version)
	if err != nil {
		return err
	}
	return xfs.WriteFileAtomic(path, []byte(version+"\n"), 0o644)
}

func runToolchainCommand(args []string) int {
	if len(args) == 0 || wantsHelp(args) {
		fmt.Fprintln(os.Stdout, "usage: nflow toolchain pin [version]")
		return 0
	}
	if args[0] != "pin" || len(args) > 2 {
		return fatalf("usage: nflow toolchain pin [version]")
	}
	version := currentToolchainVersion()
	if len(args) == 2 {
		version = args[len(args)-1]
	}
	if version == "" {
		return fatalf("toolchain pin requires a stable release version; this build has no release identity")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fatalf("toolchain pin: %v", err)
	}
	version, err = normalizeToolchainVersion(version)
	if err != nil {
		return fatalf("toolchain pin: %v", err)
	}
	if err := writeToolchainLock(filepath.Join(cwd, toolchainLockName), version); err != nil {
		return fatalf("toolchain pin: %v", err)
	}
	fmt.Fprintf(os.Stdout, "Pinned NFlow toolchain %s in %s\n", version, filepath.Join(cwd, toolchainLockName))
	return 0
}

func stripNFlowVersionFlag(args []string) (cleanArgs []string, version string, err error) {
	cleanArgs = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if value, ok := strings.CutPrefix(arg, "--nflow-version="); ok {
			if version != "" || value == "" {
				return nil, "", errors.New("--nflow-version requires one stable version and may be supplied only once")
			}
			version = value
			continue
		}
		if arg == "--nflow-version" {
			remaining := args[i+1:]
			if version != "" || len(remaining) == 0 || remaining[0] == "" {
				return nil, "", errors.New("--nflow-version requires one stable version and may be supplied only once")
			}
			version = remaining[0]
			i++
			continue
		}
		cleanArgs = append(cleanArgs, arg)
	}
	return cleanArgs, version, nil
}

type toolchainSelection struct {
	currentVersion string
	exactVersion   string
	constraints    []string
	selector       string
	launch         bool
}

func collectCLIConstraints(ctx context.Context, inv *invocation, files []string) ([]string, error) {
	var constraints []string
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || info.IsDir() {
			continue
		}
		more, err := collectVersionConstraints(ctx, inv, file)
		if err != nil {
			return nil, fmt.Errorf("preflight %s: %w", file, err)
		}
		constraints = append(constraints, more...)
	}
	return constraints, nil
}

func inspectToolchainSelection(ctx context.Context, inv *invocation, files []string, override string) (toolchainSelection, error) {
	lockVersion, locked, err := findToolchainLock(files)
	if err != nil {
		return toolchainSelection{}, err
	}
	selection := toolchainSelection{
		currentVersion: currentToolchainVersion(),
		exactVersion:   override,
	}
	if override != "" {
		selection.selector = "--nflow-version"
	}
	if selection.exactVersion == "" && locked {
		selection.exactVersion = lockVersion
		selection.selector = "nflow.lock"
	}
	selection.constraints, err = collectCLIConstraints(ctx, inv, files)
	if err != nil {
		return toolchainSelection{}, err
	}
	if selection.exactVersion != "" {
		selection.exactVersion, err = normalizeToolchainVersion(selection.exactVersion)
		if err != nil {
			return toolchainSelection{}, fmt.Errorf("toolchain selection: %w", err)
		}
		if len(selection.constraints) > 0 {
			matched, matchErr := versionSatisfiesConstraints(selection.exactVersion, selection.constraints)
			if matchErr != nil {
				return toolchainSelection{}, fmt.Errorf("toolchain compatibility: %w", matchErr)
			}
			if !matched {
				return toolchainSelection{}, fmt.Errorf("selected NFlow %s does not satisfy @nflow_version %q", selection.exactVersion, strings.Join(selection.constraints, "; "))
			}
		}
		selection.launch = selection.currentVersion != selection.exactVersion
		return selection, nil
	}
	if len(selection.constraints) == 0 {
		return selection, nil
	}
	if selection.currentVersion == "" {
		// A development or unknown build cannot establish compatibility with a
		// stable-version constraint, so resolve and run a matching stable core.
		selection.launch = true
		return selection, nil
	}
	matched, matchErr := versionSatisfiesConstraints(selection.currentVersion, selection.constraints)
	if matchErr != nil {
		return toolchainSelection{}, fmt.Errorf("toolchain compatibility: %w", matchErr)
	}
	selection.launch = !matched
	return selection, nil
}

func maybeLaunchToolchain(ctx context.Context, inv *invocation, args []string) (int, bool) {
	return maybeLaunchToolchainWithIO(ctx, inv, args, os.Stderr)
}

func maybeLaunchToolchainWithIO(ctx context.Context, inv *invocation, args []string, stderr io.Writer) (int, bool) {
	if len(args) > 0 && args[0] == toolchainCommand {
		return runToolchainCommand(args[1:]), true
	}
	cleanArgs, override, err := stripNFlowVersionFlag(args)
	if err != nil {
		return fatalf("%v", err), true
	}
	if len(cleanArgs) == 0 || wantsHelp(cleanArgs) || cleanArgs[0] == "help" || cleanArgs[0] == versionCommand || os.Getenv(toolchainResolvedEnv) != "" {
		return 0, false
	}
	files, err := toolchainSourceFiles(cleanArgs)
	if err != nil {
		return 0, false
	}
	selection, err := inspectToolchainSelection(ctx, inv, files, override)
	if err != nil {
		return fatalf("%v", err), true
	}
	fileRun := isFileBasedRuntimeInvocation(cleanArgs)
	if !selection.launch {
		if fileRun {
			writeToolchainRuntimeNotice(stderr, selection, selection.exactVersion)
		}
		return 0, false
	}
	resolver, err := createToolchainResolver()
	if err != nil {
		return fatalf("toolchain resolver: %v", err), true
	}
	var binary string
	selected := selection.exactVersion
	if selected != "" {
		binary, err = resolver.resolveExact(ctx, selected)
	} else {
		selected, binary, err = resolver.resolveCompatible(ctx, selection.constraints)
	}
	if err != nil {
		return fatalf("toolchain: %v", err), true
	}
	if fileRun {
		writeToolchainRuntimeNotice(stderr, selection, selected)
	}
	if selection.currentVersion == selected {
		return 0, false
	}
	return runResolvedCoreProcess(ctx, binary, cleanArgs), true
}

func isFileBasedRuntimeInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	rest := args
	switch args[0] {
	case "run":
		rest = args[1:]
	case "init", buildCommand, serveCommand, "lsp", "lint", explainCommand, "fmt", expandCommand, catalogCommand, "list", "show", completionCommand, versionCommand, "--version", "-v", "help", toolchainCommand, "self", requireCommand, "pin", "info":
		return false
	}
	if hasFlag(rest, "--info") || hasFlag(rest, "-info") {
		return false
	}
	target, _, _, err := splitArgs(rest)
	if err != nil || target == "" || isInlineSource(target) {
		return false
	}
	info, err := os.Stat(target)
	return err == nil && info.Mode().IsRegular()
}

func writeToolchainRuntimeNotice(stderr io.Writer, selection toolchainSelection, selected string) {
	current := selection.currentVersion
	if current == "" {
		current = strings.TrimSpace(compiler.Version)
		if current == "" {
			current = "unknown"
		}
	}
	if selected == "" {
		selected = current
	}
	if selected == current {
		fmt.Fprintf(stderr, "using NFlow %s", selected)
	} else {
		fmt.Fprintf(stderr, "NFlow current %s -> selected %s", current, selected)
	}
	var reasons []string
	switch selection.selector {
	case "--nflow-version":
		reasons = append(reasons, "--nflow-version "+selection.exactVersion)
	case "nflow.lock":
		reasons = append(reasons, "nflow.lock: "+selection.exactVersion)
	}
	if len(selection.constraints) > 0 {
		reasons = append(reasons, "@nflow_version \""+strings.Join(selection.constraints, "; ")+"\"")
	}
	if len(reasons) > 0 {
		fmt.Fprintf(stderr, " (%s)", strings.Join(reasons, "; "))
	}
	fmt.Fprintln(stderr)
}

func runResolvedCore(ctx context.Context, binary string, args []string) int {
	return runResolvedCoreWithIO(ctx, binary, args, os.Stdin, os.Stdout, os.Stderr)
}

func runResolvedCoreWithIO(ctx context.Context, binary string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	env := os.Environ()
	prefix := toolchainResolvedEnv + "="
	filtered := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	filtered = append(filtered, toolchainResolvedEnv+"=1")
	cmd.Env = filtered
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			if code := exitErr.ExitCode(); code >= 0 {
				return code
			}
		}
		fmt.Fprintf(stderr, "toolchain: launch %s: %v\n", binary, err)
		return 1
	}
	return 0
}
