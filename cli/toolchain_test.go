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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/flow/compiler"
)

type fakeRelease struct {
	version string
	archive []byte
	badSum  bool
}

func newFakeToolchainResolver(t *testing.T, releases ...fakeRelease) *toolchainResolver {
	t.Helper()
	assets := make(map[string][]byte)
	entries := make([]githubRelease, 0, len(releases))
	for _, item := range releases {
		version, err := normalizeToolchainVersion(item.version)
		if err != nil {
			t.Fatal(err)
		}
		assetVersion := strings.TrimPrefix(version, "v")
		archiveName := fmt.Sprintf("nflow_%s_linux_amd64.tar.gz", assetVersion)
		sumName := fmt.Sprintf("nflow_%s_checksums.txt", assetVersion)
		archivePath := "/assets/" + archiveName
		sumPath := "/assets/" + sumName
		assets[archivePath] = item.archive
		digest := sha256.Sum256(item.archive)
		digestText := hex.EncodeToString(digest[:])
		if item.badSum {
			digestText = strings.Repeat("0", 64)
		}
		assets[sumPath] = []byte(fmt.Sprintf("%s  %s\n", digestText, archiveName))
		entries = append(entries, githubRelease{
			TagName: version,
			Assets: []releaseAsset{
				{Name: archiveName, BrowserDownloadURL: "http://127.0.0.1" + archivePath},
				{Name: sumName, BrowserDownloadURL: "http://127.0.0.1" + sumPath},
			},
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/releases" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entries)
			return
		}
		body, ok := assets[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	resolver := &toolchainResolver{
		apiURL:   server.URL + "/releases",
		cacheDir: t.TempDir(),
		client:   server.Client(),
		goos:     "linux",
		goarch:   "amd64",
	}
	for i := range entries {
		for j := range entries[i].Assets {
			entries[i].Assets[j].BrowserDownloadURL = server.URL + strings.TrimPrefix(entries[i].Assets[j].BrowserDownloadURL, "http://127.0.0.1")
		}
	}
	// The endpoint serializes the same slice captured above, so the URLs are local.
	return resolver
}

func fakeArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "nflow", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestToolchainConstraintResolutionChoosesNewestCompatible(t *testing.T) {
	resolver := newFakeToolchainResolver(t,
		fakeRelease{version: "v0.38.0", archive: fakeArchive(t, []byte("core-38"))},
		fakeRelease{version: "v0.44.2", archive: fakeArchive(t, []byte("core-442"))},
		fakeRelease{version: "v0.50.0", archive: fakeArchive(t, []byte("core-50"))},
	)
	version, binary, err := resolver.resolveCompatible(t.Context(), []string{">=0.40.0 <0.50.0"})
	if err != nil {
		t.Fatal(err)
	}
	if version != "v0.44.2" {
		t.Fatalf("resolved version = %s, want v0.44.2", version)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "core-442" {
		t.Fatalf("cached core = %q", data)
	}
}

func TestToolchainExactCompatibleCacheSkipsReleaseLookup(t *testing.T) {
	resolver := newFakeToolchainResolver(t, fakeRelease{
		version: "v0.44.2", archive: fakeArchive(t, []byte("verified-core")),
	})
	if _, err := resolver.resolveExact(t.Context(), "v0.44.2"); err != nil {
		t.Fatal(err)
	}

	requests := 0
	lookup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unexpected release lookup", http.StatusInternalServerError)
	}))
	defer lookup.Close()
	resolver.apiURL = lookup.URL + "/releases"
	resolver.client = lookup.Client()

	version, binary, err := resolver.resolveCompatible(t.Context(), []string{"=0.44.2"})
	if err != nil {
		t.Fatal(err)
	}
	if version != "v0.44.2" {
		t.Fatalf("resolved version = %s, want v0.44.2", version)
	}
	if requests != 0 {
		t.Fatalf("made %d release API requests on an exact verified cache hit", requests)
	}
	data, err := os.ReadFile(binary)
	if err != nil || string(data) != "verified-core" {
		t.Fatalf("cached core = %q, err=%v", data, err)
	}
}

func TestToolchainCachedBinaryReusesExecutableWithoutArchive(t *testing.T) {
	resolver := newFakeToolchainResolver(t, fakeRelease{
		version: "v0.44.2", archive: fakeArchive(t, []byte("verified-core")),
	})
	binaryPath, err := resolver.resolveExact(t.Context(), "v0.44.2")
	if err != nil {
		t.Fatal(err)
	}
	archiveName, _, _ := resolver.platformNames("v0.44.2")
	if err := os.Remove(filepath.Join(resolver.platformCacheDir("v0.44.2"), archiveName)); err != nil {
		t.Fatal(err)
	}
	// A warm cache with its verified executable marker must not need the archive.
	for i := range 2 {
		cachedPath, err := resolver.cachedBinary("v0.44.2")
		if err != nil {
			t.Fatalf("cachedBinary call %d: %v", i+1, err)
		}
		if cachedPath != binaryPath {
			t.Fatalf("cached path = %q, want %q", cachedPath, binaryPath)
		}
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil || string(data) != "verified-core" {
		t.Fatalf("cached core = %q, err=%v", data, err)
	}
}

func TestToolchainLegacyCacheDoesNotAdoptExecutableWithoutTrustedDigest(t *testing.T) {
	resolver := newFakeToolchainResolver(t, fakeRelease{
		version: "v0.44.2", archive: fakeArchive(t, []byte("verified-core")),
	})
	binaryPath, err := resolver.resolveExact(t.Context(), "v0.44.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(binaryPath + ".sha256"); err != nil {
		t.Fatal(err)
	}

	if _, err := resolver.cachedBinary("v0.44.2"); err == nil {
		t.Fatal("legacy executable without a digest was silently adopted")
	}
	if _, err := os.Stat(binaryPath + ".sha256"); !os.IsNotExist(err) {
		t.Fatalf("legacy cache created an executable digest without official verification: %v", err)
	}

	// The resolver must use the official manifest and archive to migrate this
	// legacy entry before it writes a new executable digest marker.
	if _, err := resolver.resolveExact(t.Context(), "v0.44.2"); err != nil {
		t.Fatalf("verified reinstall of legacy cache: %v", err)
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil || string(data) != "verified-core" {
		t.Fatalf("migrated core = %q, err=%v", data, err)
	}
	if valid, present := verifyCachedToolchainBinary(binaryPath); !valid || !present {
		t.Fatal("verified reinstall did not write a valid executable digest")
	}
}

func TestToolchainLegacyCacheRejectsCorruptExecutableAndArchiveSidecar(t *testing.T) {
	resolver := newFakeToolchainResolver(t, fakeRelease{
		version: "v0.44.2", archive: fakeArchive(t, []byte("verified-core")),
	})
	binaryPath, err := resolver.resolveExact(t.Context(), "v0.44.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(binaryPath + ".sha256"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("tampered-core"), 0o600); err != nil {
		t.Fatal(err)
	}
	archiveName, _, _ := resolver.platformNames("v0.44.2")
	archivePath := filepath.Join(resolver.platformCacheDir("v0.44.2"), archiveName)
	tamperedArchive := fakeArchive(t, []byte("attacker-core"))
	if err := os.WriteFile(archivePath, tamperedArchive, 0o600); err != nil {
		t.Fatal(err)
	}
	archiveDigest := sha256.Sum256(tamperedArchive)
	if err := os.WriteFile(archivePath+".sha256", []byte(hex.EncodeToString(archiveDigest[:])+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := resolver.cachedBinary("v0.44.2"); err == nil {
		t.Fatal("legacy executable and locally consistent but tampered archive were silently adopted")
	}
	if _, err := os.Stat(binaryPath + ".sha256"); !os.IsNotExist(err) {
		t.Fatalf("tampered legacy cache created an executable digest without official verification: %v", err)
	}

	if _, err := resolver.resolveExact(t.Context(), "v0.44.2"); err != nil {
		t.Fatalf("official repair of tampered legacy cache: %v", err)
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil || string(data) != "verified-core" {
		t.Fatalf("repaired core = %q, err=%v", data, err)
	}
	if valid, present := verifyCachedToolchainBinary(binaryPath); !valid || !present {
		t.Fatal("official repair did not write a valid executable digest")
	}
}

func TestToolchainCachedBinaryRepairsCorruptExecutable(t *testing.T) {
	resolver := newFakeToolchainResolver(t, fakeRelease{
		version: "v0.44.2", archive: fakeArchive(t, []byte("verified-core")),
	})
	binaryPath, err := resolver.resolveExact(t.Context(), "v0.44.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("damaged-core"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.cachedBinary("v0.44.2"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil || string(data) != "verified-core" {
		t.Fatalf("repaired core = %q, err=%v", data, err)
	}
}

func TestToolchainPlatformAssetNamesAndWindowsZipExtraction(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch, archive string
	}{
		{"linux", "amd64", "nflow_0.44.2_linux_amd64.tar.gz"},
		{"darwin", "arm64", "nflow_0.44.2_darwin_arm64.tar.gz"},
		{windowsOS, "amd64", "nflow_0.44.2_windows_amd64.zip"},
	} {
		resolver := &toolchainResolver{goos: tc.goos, goarch: tc.goarch}
		archiveName, _, _ := resolver.platformNames("v0.44.2")
		if archiveName != tc.archive {
			t.Errorf("%s/%s archive = %q, want %q", tc.goos, tc.goarch, archiveName, tc.archive)
		}
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("nflow.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("windows-core")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	binary, err := extractReleaseBinary(archive.Bytes(), windowsOS, "nflow.exe")
	if err != nil || string(binary) != "windows-core" {
		t.Fatalf("Windows ZIP extraction = (%q, %v)", binary, err)
	}
}

func TestToolchainChecksumMismatchFailsClosed(t *testing.T) {
	resolver := newFakeToolchainResolver(t, fakeRelease{
		version: "v0.44.2", archive: fakeArchive(t, []byte("untrusted")), badSum: true,
	})
	_, err := resolver.resolveExact(t.Context(), "v0.44.2")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("resolveExact error = %v, want checksum mismatch", err)
	}
	if _, statErr := os.Stat(resolver.platformCacheDir("v0.44.2")); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched release left cache entries behind: %v", statErr)
	}
}

func TestToolchainPinLockAndOfflineVerifiedCache(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, toolchainLockName)
	if err := writeToolchainLock(lock, "0.44.2"); err != nil {
		t.Fatal(err)
	}
	version, found, err := nearestToolchainLock(root)
	if err != nil || !found || version != "v0.44.2" {
		t.Fatalf("nearest lock = (%q, %t, %v)", version, found, err)
	}
	resolver := newFakeToolchainResolver(t, fakeRelease{version: version, archive: fakeArchive(t, []byte("verified-core"))})
	binary, err := resolver.resolveExact(t.Context(), version)
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot := resolver.cacheDir
	offline := &toolchainResolver{
		apiURL: "http://127.0.0.1:1/releases", cacheDir: cacheRoot,
		client: &http.Client{Timeout: 100 * time.Millisecond}, goos: "linux", goarch: "amd64",
	}
	cached, err := offline.resolveExact(t.Context(), version)
	if err != nil {
		t.Fatalf("cached exact pin failed offline: %v", err)
	}
	cachedBytes, err := os.ReadFile(cached)
	if err != nil || string(cachedBytes) != "verified-core" {
		t.Fatalf("offline cached core = %q, err=%v", cachedBytes, err)
	}
	resolved, path, err := offline.resolveCompatible(t.Context(), []string{">=0.44.0 <0.45.0"})
	if err != nil || resolved != version || path == "" {
		t.Fatalf("offline compatible cache = (%q, %q, %v)", resolved, path, err)
	}
	noCache := *offline
	noCache.cacheDir = t.TempDir()
	if _, _, err := noCache.resolveCompatible(t.Context(), []string{">=0.44.0 <0.45.0"}); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("uncached offline resolution error = %v", err)
	}
	if binary == "" {
		t.Fatal("initial exact pin returned an empty path")
	}
	if err := os.WriteFile(lock, []byte("not-a-version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nearestToolchainLock(root); err == nil {
		t.Fatal("malformed lock was accepted")
	}
}

func TestToolchainGlobalVersionOverrideIsConsumedAndPreservedForHandoff(t *testing.T) {
	args := []string{"--nflow-version", "v0.44.2", "run", "workflow.nflow", "a; touch /tmp/should-not-run"}
	clean, version, err := stripNFlowVersionFlag(args)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v0.44.2" {
		t.Fatalf("override = %q", version)
	}
	if !reflect.DeepEqual(clean, []string{"run", "workflow.nflow", "a; touch /tmp/should-not-run"}) {
		t.Fatalf("clean args = %#v", clean)
	}
	if _, _, err := stripNFlowVersionFlag([]string{"--nflow-version=v0.44.2", "--nflow-version", "v0.44.2"}); err == nil {
		t.Fatal("duplicate --nflow-version was accepted")
	}
}

func TestMaybeLaunchToolchainUsesExactOverrideAndStripsSelectorForHandoff(t *testing.T) {
	oldVersion := compiler.Version
	compiler.Version = "dev"
	t.Cleanup(func() { compiler.Version = oldVersion })
	resolver := newFakeToolchainResolver(t, fakeRelease{version: "v0.44.2", archive: fakeArchive(t, []byte("selected-core"))})
	oldFactory := createToolchainResolver
	createToolchainResolver = func() (*toolchainResolver, error) { return resolver, nil }
	t.Cleanup(func() { createToolchainResolver = oldFactory })
	oldRunner := runResolvedCoreProcess
	var selectedBinary string
	var forwarded []string
	runResolvedCoreProcess = func(_ context.Context, binary string, args []string) int {
		selectedBinary = binary
		forwarded = append([]string(nil), args...)
		return 9
	}
	t.Cleanup(func() { runResolvedCoreProcess = oldRunner })

	root := t.TempDir()
	source := []byte("@nflow_version \">=0.44.0 <0.45.0\"\nconst @{ value: \"ok\" }\n")
	path := filepath.Join(root, "workflow.nflow")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--nflow-version", "v0.44.2", "run", path, "; touch /tmp/nflow-should-not-shell"}
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var handled bool
	code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
		result, wasHandled := maybeLaunchToolchain(ctx, inv, args)
		handled = wasHandled
		return result
	})
	if code != 9 || !handled {
		t.Fatalf("override launch = (code %d, handled %t)", code, handled)
	}
	if selectedBinary == "" || !strings.Contains(selectedBinary, filepath.Join("v0.44.2", "linux-amd64")) {
		t.Fatalf("selected binary = %q", selectedBinary)
	}
	expectedForwarded := []string{"run", path, "; touch /tmp/nflow-should-not-shell"}
	if !reflect.DeepEqual(forwarded, expectedForwarded) {
		t.Fatalf("forwarded args = %#v, want %#v", forwarded, expectedForwarded)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(source, after) {
		t.Fatalf("source changed during explicit selection: %q, err=%v", after, err)
	}
	if _, err := os.Stat(filepath.Join(root, toolchainLockName)); !os.IsNotExist(err) {
		t.Fatalf("--nflow-version unexpectedly wrote nflow.lock: %v", err)
	}
}

func TestToolchainCurrentCompatibleFmtLintRunDoNotDownloadOrModifyFiles(t *testing.T) {
	oldVersion := compiler.Version
	compiler.Version = "v0.44.2"
	t.Cleanup(func() { compiler.Version = oldVersion })
	oldFactory := createToolchainResolver
	calls := 0
	createToolchainResolver = func() (*toolchainResolver, error) {
		calls++
		return nil, errors.New("unexpected release lookup")
	}
	t.Cleanup(func() { createToolchainResolver = oldFactory })

	root := t.TempDir()
	source := []byte("@nflow_version \">=0.44.0 <0.45.0\"\nconst @{ value: \"ok\" }\n")
	path := filepath.Join(root, "workflow.nflow")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"fmt", path, "--check"}, {"lint", path}, {"run", path}} {
		inv, err := newNativeInvocation(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var handled bool
		code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
			var result int
			result, handled = maybeLaunchToolchain(ctx, inv, args)
			return result
		})
		if code != 0 || handled {
			t.Fatalf("maybeLaunchToolchain(%v) = code %d, handled %t", args, code, handled)
		}
	}
	if calls != 0 {
		t.Fatalf("release resolver called %d times for a compatible installed core", calls)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(source, after) {
		t.Fatalf("source changed during preflight: %q, err=%v", after, err)
	}
	if _, err := os.Stat(filepath.Join(root, toolchainLockName)); !os.IsNotExist(err) {
		t.Fatalf("preflight unexpectedly created nflow.lock: %v", err)
	}
}

func TestToolchainRuntimeVersionNoticeCurrentMatchAndSwitch(t *testing.T) {
	previousLogger := slog.Default()
	t.Setenv("LOG_LEVEL", "info")
	setupLogger()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	tests := []struct {
		name, current, declared, selected, wantNotice string
		wantChild                                     bool
	}{
		{
			name:     "current version satisfies pinned source",
			current:  "v0.44.2",
			declared: "=0.44.2",
			selected: "v0.44.2",
		},
		{
			name:       "bare file switches to declared version",
			current:    "v0.44.2",
			declared:   "=0.44.1",
			selected:   "v0.44.1",
			wantNotice: "NFlow current v0.44.2 -> selected v0.44.1 (@nflow_version \"=0.44.1\")\n",
			wantChild:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldVersion := compiler.Version
			compiler.Version = tc.current
			t.Cleanup(func() { compiler.Version = oldVersion })

			root := t.TempDir()
			path := filepath.Join(root, "workflow.nflow")
			source := []byte(fmt.Sprintf("@nflow_version %q\ntrue\n", tc.declared))
			if err := os.WriteFile(path, source, 0o600); err != nil {
				t.Fatal(err)
			}

			oldFactory := createToolchainResolver
			createToolchainResolver = func() (*toolchainResolver, error) {
				if !tc.wantChild {
					return nil, errors.New("unexpected resolver call")
				}
				return newFakeToolchainResolver(t, fakeRelease{
					version: tc.selected,
					archive: fakeArchive(t, []byte("selected-core")),
				}), nil
			}
			t.Cleanup(func() { createToolchainResolver = oldFactory })

			oldRunner := runResolvedCoreProcess
			var childArgs []string
			runResolvedCoreProcess = func(_ context.Context, binary string, args []string) int {
				if !strings.Contains(binary, filepath.Join(tc.selected, "linux-amd64")) {
					t.Errorf("resolved binary = %q, does not identify %s", binary, tc.selected)
				}
				childArgs = append([]string(nil), args...)
				return 0
			}
			t.Cleanup(func() { runResolvedCoreProcess = oldRunner })

			inv, err := newNativeInvocation(nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			var handled bool
			code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
				var result int
				result, handled = maybeLaunchToolchainWithIO(ctx, inv, []string{path}, &stderr)
				return result
			})
			if code != 0 || handled != tc.wantChild {
				t.Fatalf("bare-file dispatch = (code %d, handled %t), want (0, %t)", code, handled, tc.wantChild)
			}
			if stderr.String() != tc.wantNotice {
				t.Fatalf("runtime notice = %q, want %q", stderr.String(), tc.wantNotice)
			}
			if stdout.Len() != 0 {
				t.Fatalf("runtime notice leaked to stdout: %q", stdout.String())
			}
			if tc.wantChild && !reflect.DeepEqual(childArgs, []string{path}) {
				t.Fatalf("child args = %#v, want bare-file args %#v", childArgs, []string{path})
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(source, after) {
				t.Fatalf("runtime preflight changed source: %q, err=%v", after, err)
			}
			if _, err := os.Stat(filepath.Join(root, toolchainLockName)); !os.IsNotExist(err) {
				t.Fatalf("runtime preflight unexpectedly created nflow.lock: %v", err)
			}
		})
	}
}

func TestToolchainRuntimeNoticeExplicitSelectorsAndNoDirective(t *testing.T) {
	tests := []struct {
		name, selector, exact, want string
	}{
		{name: "no directive", want: "using NFlow v0.44.2\n"},
		{name: "lock", selector: "nflow.lock", exact: "v0.44.2", want: "using NFlow v0.44.2 (nflow.lock: v0.44.2)\n"},
		{name: "override", selector: "--nflow-version", exact: "v0.44.2", want: "using NFlow v0.44.2 (--nflow-version v0.44.2)\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			writeToolchainRuntimeNotice(&stderr, toolchainSelection{
				currentVersion: "v0.44.2",
				exactVersion:   tc.exact,
				selector:       tc.selector,
			}, tc.exact)
			if stderr.String() != tc.want {
				t.Fatalf("notice = %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestToolchainDevBuildDispatchesDeclaredStableCore(t *testing.T) {
	oldVersion := compiler.Version
	compiler.Version = "dev"
	t.Cleanup(func() { compiler.Version = oldVersion })

	root := t.TempDir()
	path := filepath.Join(root, "workflow.nflow")
	source := []byte("@nflow_version \"=0.44.1\"\ntrue\n")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := newFakeToolchainResolver(t,
		fakeRelease{version: "v0.44.1", archive: fakeArchive(t, []byte("core-441"))},
		fakeRelease{version: "v0.44.2", archive: fakeArchive(t, []byte("core-442"))},
	)
	oldFactory := createToolchainResolver
	createToolchainResolver = func() (*toolchainResolver, error) { return resolver, nil }
	t.Cleanup(func() { createToolchainResolver = oldFactory })
	oldRunner := runResolvedCoreProcess
	var selectedBinary string
	var childArgs []string
	runResolvedCoreProcess = func(_ context.Context, binary string, args []string) int {
		selectedBinary = binary
		childArgs = append([]string(nil), args...)
		return 0
	}
	t.Cleanup(func() { runResolvedCoreProcess = oldRunner })

	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	var handled bool
	code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
		var result int
		result, handled = maybeLaunchToolchainWithIO(ctx, inv, []string{path}, &stderr)
		return result
	})
	if code != 0 || !handled {
		t.Fatalf("dev launcher = (code %d, handled %t), want (0, true)", code, handled)
	}
	if !strings.Contains(selectedBinary, filepath.Join("v0.44.1", "linux-amd64")) {
		t.Fatalf("selected binary = %q, want the v0.44.1 child core", selectedBinary)
	}
	if !reflect.DeepEqual(childArgs, []string{path}) {
		t.Fatalf("child args = %#v, want %#v", childArgs, []string{path})
	}
	wantNotice := "NFlow current dev -> selected v0.44.1 (@nflow_version \"=0.44.1\")\n"
	if stderr.String() != wantNotice {
		t.Fatalf("dev runtime notice = %q, want %q", stderr.String(), wantNotice)
	}
	if strings.Contains(stderr.String(), "using NFlow dev") {
		t.Fatalf("runtime notice hides the selected child version: %q", stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(source, after) {
		t.Fatalf("dev preflight changed source: %q, err=%v", after, err)
	}
	if _, err := os.Stat(filepath.Join(root, toolchainLockName)); !os.IsNotExist(err) {
		t.Fatalf("dev preflight unexpectedly created nflow.lock: %v", err)
	}
}

func TestToolchainDevBuildWithoutSelectorStaysDev(t *testing.T) {
	previousLogger := slog.Default()
	t.Setenv("LOG_LEVEL", "info")
	setupLogger()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	oldVersion := compiler.Version
	compiler.Version = "dev"
	t.Cleanup(func() { compiler.Version = oldVersion })
	root := t.TempDir()
	path := filepath.Join(root, "workflow.nflow")
	if err := os.WriteFile(path, []byte("true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldFactory := createToolchainResolver
	resolverCalls := 0
	createToolchainResolver = func() (*toolchainResolver, error) {
		resolverCalls++
		return nil, errors.New("unexpected resolver call")
	}
	t.Cleanup(func() { createToolchainResolver = oldFactory })
	oldRunner := runResolvedCoreProcess
	runnerCalls := 0
	runResolvedCoreProcess = func(context.Context, string, []string) int {
		runnerCalls++
		return 1
	}
	t.Cleanup(func() { runResolvedCoreProcess = oldRunner })

	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	var handled bool
	code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
		var result int
		result, handled = maybeLaunchToolchainWithIO(ctx, inv, []string{path}, &stderr)
		return result
	})
	if code != 0 || handled || resolverCalls != 0 || runnerCalls != 0 {
		t.Fatalf("unselected dev run = (code %d, handled %t, resolver calls %d, child calls %d)", code, handled, resolverCalls, runnerCalls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unselected dev notice at info level = %q, want no notice", stderr.String())
	}

	t.Setenv("LOG_LEVEL", "debug")
	setupLogger()
	var debugStderr bytes.Buffer
	code, handled = maybeLaunchToolchainWithIO(t.Context(), inv, []string{path}, &debugStderr)
	if code != 0 || handled || debugStderr.String() != "using NFlow dev\n" {
		t.Fatalf("unselected dev notice at debug level = (code %d, handled %t, notice %q), want (0, false, %q)", code, handled, debugStderr.String(), "using NFlow dev\n")
	}
}

func TestToolchainDevBuildHonorsExactLock(t *testing.T) {
	oldVersion := compiler.Version
	compiler.Version = "dev"
	t.Cleanup(func() { compiler.Version = oldVersion })
	root := t.TempDir()
	t.Chdir(root)
	if err := writeToolchainLock(filepath.Join(root, toolchainLockName), "v0.44.2"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "workflow.nflow")
	source := []byte("@nflow_version \"=0.44.2\"\ntrue\n")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := newFakeToolchainResolver(t, fakeRelease{version: "v0.44.2", archive: fakeArchive(t, []byte("core-442"))})
	oldFactory := createToolchainResolver
	createToolchainResolver = func() (*toolchainResolver, error) { return resolver, nil }
	t.Cleanup(func() { createToolchainResolver = oldFactory })
	oldRunner := runResolvedCoreProcess
	var selectedBinary string
	runResolvedCoreProcess = func(_ context.Context, binary string, _ []string) int {
		selectedBinary = binary
		return 0
	}
	t.Cleanup(func() { runResolvedCoreProcess = oldRunner })

	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	var handled bool
	code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
		var result int
		result, handled = maybeLaunchToolchainWithIO(ctx, inv, []string{path}, &stderr)
		return result
	})
	if code != 0 || !handled || !strings.Contains(selectedBinary, filepath.Join("v0.44.2", "linux-amd64")) {
		t.Fatalf("locked dev selection = (code %d, handled %t, binary %q)", code, handled, selectedBinary)
	}
	wantNotice := "NFlow current dev -> selected v0.44.2 (nflow.lock: v0.44.2; @nflow_version \"=0.44.2\")\n"
	if stderr.String() != wantNotice {
		t.Fatalf("locked dev notice = %q, want %q", stderr.String(), wantNotice)
	}
}

func TestToolchainDevBuildRejectsIncompatibleIncludedConstraints(t *testing.T) {
	oldVersion := compiler.Version
	compiler.Version = "dev"
	t.Cleanup(func() { compiler.Version = oldVersion })
	root := t.TempDir()
	t.Chdir(root)
	child := filepath.Join(root, "child.nflow")
	if err := os.WriteFile(child, []byte("@nflow_version \"=0.44.2\"\ntrue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main.nflow")
	source := []byte("@nflow_version \"=0.44.1\"\n@include ./child.nflow AS child\ntrue\n")
	if err := os.WriteFile(main, source, 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := newFakeToolchainResolver(t,
		fakeRelease{version: "v0.44.1", archive: fakeArchive(t, []byte("core-441"))},
		fakeRelease{version: "v0.44.2", archive: fakeArchive(t, []byte("core-442"))},
	)
	oldFactory := createToolchainResolver
	createToolchainResolver = func() (*toolchainResolver, error) { return resolver, nil }
	t.Cleanup(func() { createToolchainResolver = oldFactory })
	oldRunner := runResolvedCoreProcess
	runnerCalls := 0
	runResolvedCoreProcess = func(context.Context, string, []string) int {
		runnerCalls++
		return 0
	}
	t.Cleanup(func() { runResolvedCoreProcess = oldRunner })

	errFile, err := os.CreateTemp(t.TempDir(), "toolchain-error-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer errFile.Close()
	oldStderr := os.Stderr
	os.Stderr = errFile
	t.Cleanup(func() { os.Stderr = oldStderr })
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var runtimeStderr bytes.Buffer
	var handled bool
	code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
		var result int
		result, handled = maybeLaunchToolchainWithIO(ctx, inv, []string{main}, &runtimeStderr)
		return result
	})
	if err := errFile.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := errFile.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	message, err := io.ReadAll(errFile)
	if err != nil {
		t.Fatal(err)
	}
	if code != 2 || !handled || runnerCalls != 0 {
		t.Fatalf("incompatible dev selection = (code %d, handled %t, child calls %d)", code, handled, runnerCalls)
	}
	for _, required := range []string{"no official stable NFlow release satisfies @nflow_version", "=0.44.1", "=0.44.2"} {
		if !strings.Contains(string(message), required) {
			t.Fatalf("mismatch diagnostic %q does not contain %q", message, required)
		}
	}
	if runtimeStderr.Len() != 0 {
		t.Fatalf("failed selection wrote a success notice: %q", runtimeStderr.String())
	}
}

func TestToolchainPreflightReadsIncludedConstraintsWithCorePreprocessor(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child.nflow")
	if err := os.WriteFile(child, []byte("@nflow_version \">=0.44.2 <0.45.0\"\nruntime.noop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main.nflow")
	if err := os.WriteFile(main, []byte("@include ./child.nflow AS child\nruntime.noop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var constraints []string
	code := runInvocation(t.Context(), inv, func(ctx context.Context) int {
		var inspectErr error
		constraints, inspectErr = collectVersionConstraints(ctx, inv, main)
		if inspectErr != nil {
			t.Errorf("collectVersionConstraints: %v", inspectErr)
			return 1
		}
		return 0
	})
	if code != 0 || !reflect.DeepEqual(constraints, []string{">=0.44.2 <0.45.0"}) {
		t.Fatalf("included constraints = %#v, code %d", constraints, code)
	}
}

func TestToolchainRecursionGuardSkipsResolution(t *testing.T) {
	t.Setenv(toolchainResolvedEnv, "1")
	oldFactory := createToolchainResolver
	createToolchainResolver = func() (*toolchainResolver, error) {
		return nil, errors.New("resolver must not run")
	}
	t.Cleanup(func() { createToolchainResolver = oldFactory })
	code, handled := maybeLaunchToolchain(t.Context(), nil, []string{"--nflow-version", "v0.44.2", "lsp"})
	if code != 0 || handled {
		t.Fatalf("recursion-guarded launch = (%d, %t), want (0, false)", code, handled)
	}
}

func TestRunResolvedCorePassesArgvStreamsStatusAndNoShell(t *testing.T) {
	binary := buildToolchainTestHelper(t)
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("NFLOW_TEST_HELPER_RESULT", resultPath)
	sentinel := filepath.Join(filepath.Dir(resultPath), "not-created")
	args := []string{"fmt", "file with spaces.nflow", "; touch " + sentinel, "--check"}
	var stdout, stderr bytes.Buffer
	code := runResolvedCoreWithIO(t.Context(), binary, args, strings.NewReader("stdin-preserved"), &stdout, &stderr)
	if code != 7 {
		t.Fatalf("child exit status = %d, want 7", code)
	}
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Args   []string `json:"args"`
		Stdin  string   `json:"stdin"`
		Marker string   `json:"marker"`
	}
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Args, args) {
		t.Fatalf("child argv = %#v, want %#v", result.Args, args)
	}
	if result.Stdin != "stdin-preserved" || result.Marker != "1" {
		t.Fatalf("child stdin/recursion marker = (%q, %q)", result.Stdin, result.Marker)
	}
	if stdout.String() != "child-stdout:stdin-preserved" || stderr.String() != "child-stderr" {
		t.Fatalf("child streams = stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("argument was interpreted by a shell: %v", err)
	}
}

func buildToolchainTestHelper(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := `package main
import (
  "encoding/json"
  "io"
  "os"
)
func main() {
  input, _ := io.ReadAll(os.Stdin)
  result := map[string]any{"args": os.Args[1:], "stdin": string(input), "marker": os.Getenv("NFLOW_TOOLCHAIN_RESOLVED")}
  data, _ := json.Marshal(result)
  _ = os.WriteFile(os.Getenv("NFLOW_TEST_HELPER_RESULT"), data, 0600)
  _, _ = os.Stdout.Write(append([]byte("child-stdout:"), input...))
  _, _ = os.Stderr.Write([]byte("child-stderr"))
  os.Exit(7)
}
`
	srcPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "helper")
	if runtime.GOOS == windowsOS {
		binary += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binary, srcPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build subprocess helper: %v\n%s", err, output)
	}
	return binary
}
