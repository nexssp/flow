package cli

import (
	"archive/zip"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

func TestHarness_LoosePackage_AutoBundling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration harness test in short mode")
	}

	tempDir := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(tempDir, "cache"))

	if err := os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	localPkg := filepath.Join(tempDir, "mylocal")
	if err := os.MkdirAll(localPkg, 0o755); err != nil {
		t.Fatal(err)
	}

	libGo := `package mylocal

import (
	"context"
	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "mylocal"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	act := action.New("mylocal.echo", func(_ context.Context, in map[string]any) (map[string]any, error) {
		return map[string]any{"val": "loose_ok"}, nil
	}).Build()
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}},
	}
}
`
	if err := os.WriteFile(filepath.Join(localPkg, "library.go"), []byte(libGo), 0o600); err != nil {
		t.Fatal(err)
	}

	nflowContent := `@require ./mylocal
@assert: result.val == "loose_ok"
{} -> mylocal.echo
`
	nflowPath := filepath.Join(tempDir, "test.nflow")
	if err := os.WriteFile(nflowPath, []byte(nflowContent), 0o600); err != nil {
		t.Fatal(err)
	}

	code := runFlow([]string{nflowPath})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	_, stderr, lintCode := captureLintOutput(t, func() int {
		return runLint([]string{nflowPath})
	})
	if lintCode != 0 {
		t.Fatalf("lint through external @require harness returned %d: %s", lintCode, stderr)
	}
}

func TestHarness_Monorepo_RootGoMod(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration harness test in short mode")
	}

	tempDir := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(tempDir, "cache"))

	if err := os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	rootMod := `module testmonorepo
go 1.23
`
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(rootMod), 0o600); err != nil {
		t.Fatal(err)
	}

	subPkg := filepath.Join(tempDir, "sub", "worker")
	if err := os.MkdirAll(subPkg, 0o755); err != nil {
		t.Fatal(err)
	}

	libGo := `package worker

import (
	"context"
	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "worker"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	act := action.New("worker.run", func(_ context.Context, in map[string]any) (map[string]any, error) {
		return map[string]any{"status": "monorepo_ok"}, nil
	}).Build()
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}},
	}
}
`
	if err := os.WriteFile(filepath.Join(subPkg, "library.go"), []byte(libGo), 0o600); err != nil {
		t.Fatal(err)
	}

	nflowContent := `@require ./sub/worker
@assert: result.status == "monorepo_ok"
{} -> worker.run
`
	nflowPath := filepath.Join(tempDir, "test.nflow")
	if err := os.WriteFile(nflowPath, []byte(nflowContent), 0o600); err != nil {
		t.Fatal(err)
	}

	code := runFlow([]string{nflowPath})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestHarness_OptionsChange_InvalidatesCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration harness test in short mode")
	}

	tempDir := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(tempDir, "cache"))

	if err := os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	localPkg := filepath.Join(tempDir, "optlocal")
	if err := os.MkdirAll(localPkg, 0o755); err != nil {
		t.Fatal(err)
	}

	libGo := `package optlocal

import (
	"context"
	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "optlocal"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	val := opts["custom_option"]
	act := action.New("optlocal.get", func(_ context.Context, in map[string]any) (map[string]any, error) {
		return map[string]any{"opt": val}, nil
	}).Build()
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}},
	}
}
`
	if err := os.WriteFile(filepath.Join(localPkg, "library.go"), []byte(libGo), 0o600); err != nil {
		t.Fatal(err)
	}

	nflowPath := filepath.Join(tempDir, "test.nflow")

	nflow1 := `@require ./optlocal { custom_option: "version_A" }
@assert: result.opt == "version_A"
{} -> optlocal.get
`
	if err := os.WriteFile(nflowPath, []byte(nflow1), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFlow([]string{nflowPath}); code != 0 {
		t.Fatalf("initial run failed with code %d", code)
	}

	nflow2 := `@require ./optlocal { custom_option: "version_B" }
@assert: result.opt == "version_B"
{} -> optlocal.get
`
	if err := os.WriteFile(nflowPath, []byte(nflow2), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFlow([]string{nflowPath}); code != 0 {
		t.Fatalf("second run failed; harness did not recompile with modified option: %d", code)
	}
}

func TestHarness_SourceEdit_InvalidatesCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration harness test in short mode")
	}

	tempDir := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(tempDir, "cache"))

	if err := os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	localPkg := filepath.Join(tempDir, "editlocal")
	if err := os.MkdirAll(localPkg, 0o755); err != nil {
		t.Fatal(err)
	}

	libPath := filepath.Join(localPkg, "library.go")
	libGoV1 := `package editlocal

import (
	"context"
	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "editlocal"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	act := action.New("editlocal.run", func(_ context.Context, in map[string]any) (map[string]any, error) {
		return map[string]any{"version": "v1"}, nil
	}).Build()
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}},
	}
}
`
	if err := os.WriteFile(libPath, []byte(libGoV1), 0o600); err != nil {
		t.Fatal(err)
	}

	nflowPath := filepath.Join(tempDir, "test.nflow")

	nflow1 := `@require ./editlocal
@assert: result.version == "v1"
{} -> editlocal.run
`
	if err := os.WriteFile(nflowPath, []byte(nflow1), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFlow([]string{nflowPath}); code != 0 {
		t.Fatalf("v1 run failed with code %d", code)
	}

	libGoV2 := `package editlocal

import (
	"context"
	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "editlocal"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	act := action.New("editlocal.run", func(_ context.Context, in map[string]any) (map[string]any, error) {
		return map[string]any{"version": "v2_updated"}, nil
	}).Build()
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}},
	}
}
`
	if err := os.WriteFile(libPath, []byte(libGoV2), 0o600); err != nil {
		t.Fatal(err)
	}

	nflow2 := `@require ./editlocal
@assert: result.version == "v2_updated"
{} -> editlocal.run
`
	if err := os.WriteFile(nflowPath, []byte(nflow2), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFlow([]string{nflowPath}); code != 0 {
		t.Fatalf("v2 run failed; source edit did not invalidate runner cache: %d", code)
	}
}

func TestHarness_BundleImportPath_Variants(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "RootModule_AppendsNexssflow",
			input:    "github.com/nexssp/transport",
			expected: "github.com/nexssp/transport/nexssflow",
		},
		{
			name:     "RootModule_AlreadySuffixed",
			input:    "github.com/nexssp/transport/nexssflow",
			expected: "github.com/nexssp/transport/nexssflow",
		},
		{
			name:     "RootModule_PrefixVariant",
			input:    "github.com/nexssp/transport/nexssflow_v2",
			expected: "github.com/nexssp/transport/nexssflow_v2",
		},
		{
			name:     "DeepSubpackage_AppendsNexssflow",
			input:    "github.com/nexssp/transport/thttp",
			expected: "github.com/nexssp/transport/thttp/nexssflow",
		},
		{
			name:     "DeepSubpackage_AlreadySuffixed",
			input:    "github.com/nexssp/transport/thttp/nexssflow",
			expected: "github.com/nexssp/transport/thttp/nexssflow",
		},
		{
			name:     "DeepSubpackage_PrefixVariant",
			input:    "github.com/nexssp/transport/thttp/nexssflow_dev",
			expected: "github.com/nexssp/transport/thttp/nexssflow_dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := require.Requirement{Import: tt.input}
			got := bundleImportPath(req)
			if got != tt.expected {
				t.Errorf("bundleImportPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestInit_ProjectScaffolding(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	if code := runInit([]string{}); code != 0 {
		t.Fatalf("runInit failed with code %d", code)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "workflow.nflow")); err != nil {
		t.Fatalf("expected workflow.nflow to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "helpers", "library.go")); err != nil {
		t.Fatalf("expected helpers/library.go to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "README.md")); err != nil {
		t.Fatalf("expected README.md to exist: %v", err)
	}
}

func TestInit_BundleScaffolding(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	if code := runInit([]string{"--bundle"}); code != 0 {
		t.Fatalf("runInit --bundle failed with code %d", code)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "nexssflow", "library.go")); err != nil {
		t.Fatalf("expected nexssflow/library.go: %v", err)
	}

	if code := runInit([]string{"--bundle", "nexssflow_dev"}); code != 0 {
		t.Fatalf("runInit --bundle nexssflow_dev failed with code %d", code)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "nexssflow_dev", "library.go")); err != nil {
		t.Fatalf("expected nexssflow_dev/library.go: %v", err)
	}
}

func TestHarness_RemoteModulePathIdentity(t *testing.T) {
	cases := []struct {
		name string
		req  require.Requirement
		want string
	}{
		{
			name: "versioned nested module uses full path",
			req:  require.Requirement{Import: "github.com/nexssp/cost/nexssflow", Version: "v1.2.3"},
			want: "github.com/nexssp/cost/nexssflow",
		},
		{
			name: "bare repository keeps named module path",
			req:  require.Requirement{Import: "github.com/example/my-repo", Version: "v1.2.3"},
			want: "github.com/example/my-repo",
		},
		{
			name: "unversioned same-module package keeps root inference",
			req:  require.Requirement{Import: "github.com/example/my-repo/internal/worker"},
			want: "github.com/example/my-repo",
		},
		{
			name: "local package uses parsed module path",
			req:  require.Requirement{Import: "example.invalid/local/mod/nested", ModulePath: "example.invalid/local/mod", LocalPath: "/tmp/local"},
			want: "example.invalid/local/mod",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modulePathForRequirement(tc.req); got != tc.want {
				t.Fatalf("modulePathForRequirement(%+v) = %q, want %q", tc.req, got, tc.want)
			}
		})
	}

	versionedNested := require.Requirement{Import: "github.com/nexssp/cost/nexssflow", Version: "v1.2.3"}
	mod := harnessGoMod("", "v0.14.0", "", []require.Requirement{versionedNested})
	if !strings.Contains(mod, "\tgithub.com/nexssp/cost/nexssflow v1.2.3\n") {
		t.Fatalf("generated go.mod omitted the exact nested module requirement:\n%s", mod)
	}
	if strings.Contains(mod, "\tgithub.com/nexssp/cost v1.2.3\n") {
		t.Fatalf("generated go.mod truncated the nested module path:\n%s", mod)
	}

	unversionedSameModule := require.Requirement{Import: "github.com/nexssp/flow/extensions/macros"}
	mod = harnessGoMod("", "v0.14.0", "", []require.Requirement{unversionedSameModule})
	if strings.Contains(mod, "\tgithub.com/nexssp/flow/extensions/macros ") {
		t.Fatalf("unversioned Flow subpackage was treated as an independent module:\n%s", mod)
	}
	versionedFlowSubmodule := require.Requirement{
		Import:  "github.com/nexssp/flow/extensions/macros",
		Version: "v1.2.3",
	}
	mod = harnessGoMod("", "v0.14.0", "", []require.Requirement{versionedFlowSubmodule})
	if !strings.Contains(mod, "\tgithub.com/nexssp/flow/extensions/macros v1.2.3\n") {
		t.Fatalf("versioned nested module beneath Flow was suppressed as a same-module package:\n%s", mod)
	}
	if got := bundleImportPath(require.Requirement{Import: "github.com/example/my-repo", Version: "v1.2.3"}); got != "github.com/example/my-repo/nexssflow" {
		t.Fatalf("bare repository bundle import = %q; want /nexssflow convention", got)
	}
}

func TestLocalFileProxyURL(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "Windows drive-letter path",
			path: `C:\Temp\Go Proxy`,
			want: "file:///C:/Temp/Go%20Proxy",
		},
		{
			name: "Unix absolute path",
			path: "/tmp/Go Proxy",
			want: "file:///tmp/Go%20Proxy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := localFileProxyURL(tc.path); got != tc.want {
				t.Fatalf("localFileProxyURL(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func localFileProxyURL(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && path[2] == '/' {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func TestHarness_BareTargetUsesNestedRemoteModuleForRunAndBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local-proxy run/build integration test in short mode")
	}

	workDir := t.TempDir()
	proxyDir := filepath.Join(workDir, "proxy")
	const basePath = "example.invalid/nested/parent"
	const modulePath = basePath + "/nexssflow"
	const rootVersion = "v0.9.0"
	const adapterVersion = "v1.2.4"
	writeGoModuleProxy(t, proxyDir, basePath, rootVersion, map[string]string{"README.md": "the root release does not contain the adapter package"})
	writeGoModuleProxy(t, proxyDir, modulePath, adapterVersion, map[string]string{
		"library.go": testBundleSource("parent"),
	})

	proxyURL := localFileProxyURL(proxyDir)
	t.Setenv("GOPROXY", proxyURL+",off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv(harnessCacheEnv, filepath.Join(workDir, "cache"))

	flowPath := filepath.Join(workDir, "nested.nflow")
	flow := "@require " + basePath + " " + adapterVersion + ` as named { prefix: "proxy" }
@assert: result.value == "proxy:ok"
{} -> named.echo
`
	if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := runFlow([]string{flowPath}); code != 0 {
		t.Fatalf("nflow run with nested module from local proxy returned %d", code)
	}

	binaryPath := filepath.Join(workDir, "nested-flow"+exeSuffix())
	if code := runBuild([]string{flowPath, "-o", binaryPath}); code != 0 {
		t.Fatalf("nflow build with nested module from local proxy returned %d", code)
	}
	cmd := exec.CommandContext(t.Context(), binaryPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built flow failed: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(string(output), `"value":"proxy:ok"`) {
		t.Fatalf("built flow did not preserve aliased option-configured bundle output:\n%s", output)
	}
}

func TestHarness_BareTargetUsesRootModuleNexssflowPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local-proxy package resolver test in short mode")
	}
	workDir := t.TempDir()
	proxyDir := filepath.Join(workDir, "proxy")
	const modulePath = "example.invalid/rootpackage/repo"
	const version = "v1.4.2"
	writeGoModuleProxy(t, proxyDir, modulePath, version, map[string]string{
		"nexssflow/library.go": testBundleSource("repo"),
	})
	proxyURL := localFileProxyURL(proxyDir)
	t.Setenv("GOPROXY", proxyURL+",off")
	t.Setenv("GOSUMDB", "off")

	req := require.Requirement{Import: modulePath, Version: version}
	resolved, err := resolveRequirementPackages([]require.Requirement{req}, mustSelfModuleRoot(t), "", "")
	if err != nil {
		t.Fatalf("resolve bare root-module package: %v", err)
	}
	got := resolved[0]
	if got.PackagePath != modulePath+"/nexssflow" || got.ModulePath != modulePath || got.ResolvedVersion != version {
		t.Fatalf("bare root target resolved as %+v; want package %s provided by %s@%s", got, modulePath+"/nexssflow", modulePath, version)
	}
}

func TestHarness_ExplicitVariantUsesGoPackageProviderForRunAndBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local-proxy run/build integration test in short mode")
	}
	workDir := t.TempDir()
	proxyDir := filepath.Join(workDir, "proxy")
	const basePath = "example.invalid/variant/repo"
	const packagePath = basePath + "/nexssflow_v2"
	const version = "v1.3.1"
	writeGoModuleProxy(t, proxyDir, basePath, version, map[string]string{
		"nexssflow_v2/library.go": testBundleSource("variant-repo"),
	})
	proxyURL := localFileProxyURL(proxyDir)
	t.Setenv("GOPROXY", proxyURL+",off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv(harnessCacheEnv, filepath.Join(workDir, "cache"))

	flowPath := filepath.Join(workDir, "variant.nflow")
	flow := "@require " + packagePath + " " + version + ` as chosen { prefix: "variant" }
@assert: result.value == "variant:ok"
{} -> chosen.echo
`
	if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFlow([]string{flowPath}); code != 0 {
		t.Fatalf("nflow run with explicit variant package returned %d", code)
	}

	binaryPath := filepath.Join(workDir, "variant-flow"+exeSuffix())
	if code := runBuild([]string{flowPath, "-o", binaryPath}); code != 0 {
		t.Fatalf("nflow build with explicit variant package returned %d", code)
	}
	cmd := exec.CommandContext(t.Context(), binaryPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built variant flow failed: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(string(output), `"value":"variant:ok"`) {
		t.Fatalf("built variant flow returned unexpected output:\n%s", output)
	}
}

func TestHarness_LocalRootAndNestedAdapterDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local adapter discovery integration tests in short mode")
	}
	cases := []struct {
		name         string
		rootModule   bool
		nestedModule bool
	}{
		{name: "root-module package", rootModule: true},
		{name: "independent nested module", rootModule: true, nestedModule: true},
		{name: "standalone nested module", nestedModule: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			t.Setenv(harnessCacheEnv, filepath.Join(workDir, "cache"))
			const rootPath = "example.invalid/local/repo"
			if tc.rootModule {
				if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module "+rootPath+"\n\ngo 1.23\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			adapterDir := filepath.Join(workDir, "nexssflow")
			if err := os.MkdirAll(adapterDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.nestedModule {
				if err := os.WriteFile(filepath.Join(adapterDir, "go.mod"), []byte("module "+rootPath+"/nexssflow\n\ngo 1.23\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(adapterDir, "library.go"), []byte(testBundleSource("local-repo")), 0o600); err != nil {
				t.Fatal(err)
			}
			flowPath := filepath.Join(workDir, "local.nflow")
			flow := `@require ./ as local { prefix: "local" }
@assert: result.value == "local:ok"
{} -> local.echo
`
			if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
				t.Fatal(err)
			}
			if code := runFlow([]string{flowPath}); code != 0 {
				t.Fatalf("nflow run with %s returned %d", tc.name, code)
			}
		})
	}
}

func mustSelfModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := selfModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func testBundleSource(bundleID string) string {
	return fmt.Sprintf(`package nexssflow

import (
	"context"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = %q

func init() { core.Register(ID, Bundle) }

func Bundle(opts map[string]string) core.Bundle {
	prefix := opts["prefix"]
	act := action.New(ID+".echo", func(_ context.Context, _ any) (map[string]any, error) {
		return map[string]any{"value": prefix + ":ok"}, nil
	}).Build()
	return core.Bundle{ID: ID, Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}}}
}
`, bundleID)
}

func writeGoModuleProxy(t *testing.T, proxyDir, modulePath, version string, files map[string]string) {
	t.Helper()
	versionDir := filepath.Join(proxyDir, filepath.FromSlash(modulePath), "@v")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	info := `{"Version":"` + version + `","Time":"2026-10-04T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(versionDir, version+".info"), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}
	moduleFile := "module " + modulePath + "\n\ngo 1.23\n"
	if err := os.WriteFile(filepath.Join(versionDir, version+".mod"), []byte(moduleFile), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(versionDir, version+".zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(archive)
	for name, contents := range files {
		entry, err := zipWriter.Create(modulePath + "@" + version + "/" + filepath.ToSlash(name))
		if err != nil {
			_ = zipWriter.Close()
			_ = archive.Close()
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			_ = zipWriter.Close()
			_ = archive.Close()
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		_ = archive.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessKey_DistinguishesResolvedPackageAndVersion(t *testing.T) {
	base := require.Requirement{
		Import:          "example.invalid/cache/repo",
		Version:         "v1.2.3",
		PackagePath:     "example.invalid/cache/repo/nexssflow",
		ModulePath:      "example.invalid/cache/repo",
		ResolvedVersion: "v1.2.3",
	}
	baseKey := harnessKey([]require.Requirement{base}, "", nil)

	variant := base
	variant.PackagePath = "example.invalid/cache/repo/nexssflow_v2"
	if got := harnessKey([]require.Requirement{variant}, "", nil); got == baseKey {
		t.Fatal("different resolved package variants shared a harness cache key")
	}

	version := base
	version.Version = "v1.2.4"
	version.ResolvedVersion = "v1.2.4"
	if got := harnessKey([]require.Requirement{version}, "", nil); got == baseKey {
		t.Fatal("different resolved versions shared a harness cache key")
	}

	provider := base
	provider.ModulePath = "example.invalid/cache/repo/nexssflow"
	if got := harnessKey([]require.Requirement{provider}, "", nil); got == baseKey {
		t.Fatal("different Go module providers shared a harness cache key")
	}
}

func TestHarnessKey_DistinguishesNativeBundleSet(t *testing.T) {
	req := require.Requirement{
		Import:          "example.invalid/native/repo",
		Version:         "v1.0.0",
		PackagePath:     "example.invalid/native/repo/nexssflow",
		ModulePath:      "example.invalid/native/repo",
		ResolvedVersion: "v1.0.0",
	}

	withPipeline := harnessKey([]require.Requirement{req}, "", []string{"pipeline"})
	withoutNative := harnessKey([]require.Requirement{req}, "", nil)
	if withPipeline == withoutNative {
		t.Fatal("native bundle set did not affect the harness cache key")
	}

	// Ordering must not matter: the set is sorted before hashing so that
	// two callers passing the same IDs in different order share a cache.
	a := harnessKey([]require.Requirement{req}, "", []string{"pipeline", "schema"})
	b := harnessKey([]require.Requirement{req}, "", []string{"schema", "pipeline"})
	if a != b {
		t.Fatal("native bundle ordering affected the harness cache key")
	}

	// A cached harness that lacks @pipeline must not be reused for a
	// source that needs it, and vice versa.
	onlySchema := harnessKey([]require.Requirement{req}, "", []string{"schema"})
	if onlySchema == withPipeline {
		t.Fatal("different native bundle sets shared a harness cache key")
	}
}

func TestHarness_LocalExplicitVariantPathIsPreserved(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping explicit local variant integration test in short mode")
	}
	workDir := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(workDir, "cache"))
	const modulePath = "example.invalid/localvariant/repo"
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	variantDir := filepath.Join(workDir, "nexssflow_v2")
	if err := os.MkdirAll(variantDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variantDir, "library.go"), []byte(testBundleSource("local-variant")), 0o600); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(workDir, "variant.nflow")
	flow := `@require ./nexssflow_v2 as selected { prefix: "explicit" }
@assert: result.value == "explicit:ok"
{} -> selected.echo
`
	if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFlow([]string{flowPath}); code != 0 {
		t.Fatalf("nflow run with explicit local variant path returned %d", code)
	}
}

func TestHarness_UnversionedRemoteRequiresExplicitVersion(t *testing.T) {
	flowRoot := mustSelfModuleRoot(t)
	remote := require.Requirement{Import: "example.invalid/unpinned/repo"}
	if _, err := resolveRequirementPackages([]require.Requirement{remote}, flowRoot, "", ""); err == nil || !strings.Contains(err.Error(), "need an explicit version") {
		t.Fatalf("unversioned remote target error = %v; want explicit-version diagnostic", err)
	}

	flowModule, err := require.ReadModuleLine(filepath.Join(flowRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sameModule := require.Requirement{Import: flowModule + "/extensions/requiretestfixture"}
	resolved, err := resolveRequirementPackages([]require.Requirement{sameModule}, flowRoot, "", "")
	if err != nil {
		t.Fatalf("unversioned same-module subpackage was rejected: %v", err)
	}
	wantPackagePath := sameModule.Import + "/nexssflow"
	if resolved[0].PackagePath != wantPackagePath || resolved[0].ModulePath != flowModule {
		t.Fatalf("unversioned same-module package resolved as %+v (want PackagePath=%s ModulePath=%s)",
			resolved[0], wantPackagePath, flowModule)
	}
}

func TestHarness_UnversionedCurrentModulePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping current-module subpackage integration test in short mode")
	}
	workDir := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(workDir, "cache"))
	const modulePath = "example.invalid/current/repo"
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapterDir := filepath.Join(workDir, "nexssflow")
	if err := os.MkdirAll(adapterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adapterDir, "library.go"), []byte(testBundleSource("current-repo")), 0o600); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(workDir, "current.nflow")
	flow := "@require " + modulePath + ` as current { prefix: "module" }
@assert: result.value == "module:ok"
{} -> current.echo
`
	if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runFlow([]string{flowPath}); code != 0 {
		t.Fatalf("nflow run with an unversioned current-module package returned %d", code)
	}
}
