package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

func TestResolveRemotePackageProvidersBatchesGoCommands(t *testing.T) {
	first := remotePackageQuery{packagePath: "example.invalid/z/repo/nexssflow", version: "v1.2.0"}
	second := remotePackageQuery{packagePath: "example.invalid/a/repo/nexssflow_v2", version: "v1.3.0"}
	var calls [][]string
	runGo := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, slices.Clone(args))
		switch args[0] {
		case "get":
			return nil, nil
		case "list":
			return []byte(
				`{"ImportPath":"` + second.packagePath + `","Module":{"Path":"example.invalid/a/repo","Version":"v1.3.0"}}` + "\n" +
					`{"ImportPath":"` + first.packagePath + `","Module":{"Path":"example.invalid/z/repo","Version":"v1.2.0"}}` + "\n",
			), nil
		default:
			return nil, fmt.Errorf("unexpected Go command %q", args[0])
		}
	}

	got, err := resolveRemotePackageProvidersWithRunner(
		t.Context(),
		[]remotePackageQuery{first, second, first},
		"",
		"",
		"",
		runGo,
	)
	if err != nil {
		t.Fatalf("resolve remote providers: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("Go command calls = %v, want one go get and one go list", calls)
	}
	wantGet := []string{"get", second.packagePath + "@" + second.version, first.packagePath + "@" + first.version}
	if !slices.Equal(calls[0], wantGet) {
		t.Fatalf("go get arguments = %v, want %v", calls[0], wantGet)
	}
	wantList := []string{"list", "-json", second.packagePath, first.packagePath}
	if !slices.Equal(calls[1], wantList) {
		t.Fatalf("go list arguments = %v, want %v", calls[1], wantList)
	}
	if got[second] != (remotePackageProvider{path: "example.invalid/a/repo", version: "v1.3.0"}) {
		t.Fatalf("provider for %s = %+v", second.packagePath, got[second])
	}
	if got[first] != (remotePackageProvider{path: "example.invalid/z/repo", version: "v1.2.0"}) {
		t.Fatalf("provider for %s = %+v", first.packagePath, got[first])
	}
}

func TestResolveRequirementPackagesBatchesRemoteModules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local-proxy multi-package resolver test in short mode")
	}

	workDir := t.TempDir()
	proxyDir := filepath.Join(workDir, "proxy")
	firstModule := testProxyModulePath(workDir, "first/repo")
	secondModule := testProxyModulePath(workDir, "second/repo")
	const firstVersion = "v1.2.0"
	const secondVersion = "v1.3.0"
	writeGoModuleProxy(t, proxyDir, firstModule, firstVersion, map[string]string{
		"nexssflow/library.go": testBundleSource("first"),
	})
	writeGoModuleProxy(t, proxyDir, secondModule, secondVersion, map[string]string{
		"nexssflow_v2/library.go": testBundleSource("second"),
	})
	t.Setenv("GOPROXY", localFileProxyURL(proxyDir)+",off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOTOOLCHAIN", "local")

	resolved, err := resolveRequirementPackages([]require.Requirement{
		{Import: firstModule, Version: firstVersion},
		{Import: secondModule + "/nexssflow_v2", Version: secondVersion},
	}, mustSelfModuleRoot(t))
	if err != nil {
		t.Fatalf("resolve multiple remote bundles: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved requirements = %d, want 2", len(resolved))
	}
	if resolved[0].PackagePath != firstModule+"/nexssflow" || resolved[0].ModulePath != firstModule || resolved[0].ResolvedVersion != firstVersion {
		t.Errorf("first resolved requirement = %+v", resolved[0])
	}
	if resolved[1].PackagePath != secondModule+"/nexssflow_v2" || resolved[1].ModulePath != secondModule || resolved[1].ResolvedVersion != secondVersion {
		t.Errorf("second resolved requirement = %+v", resolved[1])
	}
}

func TestResolveRequirementPackagesSeparatesConflictingSharedModuleSelectors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local-proxy shared-module selector test in short mode")
	}

	workDir := t.TempDir()
	proxyDir := filepath.Join(workDir, "proxy")
	modulePath := testProxyModulePath(workDir, "shared/repo")
	const lowerVersion = "v1.2.0"
	const higherVersion = "v1.3.0"
	for _, version := range []string{lowerVersion, higherVersion} {
		writeGoModuleProxy(t, proxyDir, modulePath, version, map[string]string{
			"nexssflow/library.go":    testBundleSource("shared"),
			"nexssflow_v2/library.go": testBundleSource("shared"),
		})
	}
	t.Setenv("GOPROXY", localFileProxyURL(proxyDir)+",off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOTOOLCHAIN", "local")

	resolved, err := resolveRequirementPackages([]require.Requirement{
		{Import: modulePath, Version: lowerVersion},
		{Import: modulePath + "/nexssflow_v2", Version: higherVersion},
	}, mustSelfModuleRoot(t))
	if err != nil {
		t.Fatalf("resolve packages from shared remote module: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved requirements = %d, want 2", len(resolved))
	}
	for i, expectedVersion := range []string{lowerVersion, higherVersion} {
		if resolved[i].ModulePath != modulePath || resolved[i].ResolvedVersion != expectedVersion {
			t.Errorf("resolved requirement = %+v, want provider %s@%s", resolved[i], modulePath, expectedVersion)
		}
	}
}

func TestResolveRemotePackageProvidersRetriesConflictsByVersion(t *testing.T) {
	queries := []remotePackageQuery{
		{packagePath: "example.invalid/shared/repo/pkg_a", version: "v1.3.0"},
		{packagePath: "example.invalid/shared/repo/pkg_b", version: "v1.2.0"},
		{packagePath: "example.invalid/shared/repo/pkg_c", version: "v1.3.0"},
		{packagePath: "example.invalid/shared/repo/pkg_d", version: "v1.2.0"},
	}
	versions := make(map[string]string, len(queries))
	for _, query := range queries {
		versions[query.packagePath] = query.version
	}
	var calls [][]string
	var dirs []string
	runGo := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		calls = append(calls, slices.Clone(args))
		dirs = append(dirs, dir)
		switch args[0] {
		case "get":
			if len(calls) == 1 {
				return nil, errors.New("requested module version conflicts with another requested version")
			}
			return nil, nil
		case "list":
			var output strings.Builder
			for _, packagePath := range args[2:] {
				fmt.Fprintf(&output, `{"ImportPath":%q,"Module":{"Path":"example.invalid/shared/repo","Version":%q}}`+"\n", packagePath, versions[packagePath])
			}
			return []byte(output.String()), nil
		default:
			return nil, fmt.Errorf("unexpected Go command %q", args[0])
		}
	}

	got, err := resolveRemotePackageProvidersWithRunner(t.Context(), queries, "", "", "", runGo)
	if err != nil {
		t.Fatalf("resolve providers after selector conflict: %v", err)
	}
	wantCalls := [][]string{
		{"get", queries[0].packagePath + "@v1.3.0", queries[1].packagePath + "@v1.2.0", queries[2].packagePath + "@v1.3.0", queries[3].packagePath + "@v1.2.0"},
		{"get", queries[1].packagePath + "@v1.2.0", queries[3].packagePath + "@v1.2.0"},
		{"list", "-json", queries[1].packagePath, queries[3].packagePath},
		{"get", queries[0].packagePath + "@v1.3.0", queries[2].packagePath + "@v1.3.0"},
		{"list", "-json", queries[0].packagePath, queries[2].packagePath},
	}
	if len(calls) != len(wantCalls) {
		t.Fatalf("Go calls = %v, want %v", calls, wantCalls)
	}
	for i := range wantCalls {
		if !slices.Equal(calls[i], wantCalls[i]) {
			t.Errorf("Go call %d = %v, want %v", i, calls[i], wantCalls[i])
		}
	}
	for _, query := range queries {
		if want := (remotePackageProvider{path: "example.invalid/shared/repo", version: query.version}); got[query] != want {
			t.Errorf("provider for %s = %+v, want %+v", query.packagePath, got[query], want)
		}
	}
	for i, dir := range dirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("resolver temp directory for Go call %d still exists or stat failed: %v", i, err)
		}
	}
}

func TestResolveRemotePackageProvidersDoesNotRetryUnrelatedGetErrors(t *testing.T) {
	calls := 0
	runGo := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		if args[0] != "get" {
			t.Fatalf("unexpected retry command %v", args)
		}
		return nil, errors.New("proxy unavailable")
	}

	_, err := resolveRemotePackageProvidersWithRunner(
		t.Context(),
		[]remotePackageQuery{{packagePath: "example.invalid/repo/nexssflow", version: "v1.2.0"}},
		"", "", "", runGo,
	)
	if err == nil || !strings.Contains(err.Error(), "proxy unavailable") {
		t.Fatalf("resolver error = %v, want proxy failure", err)
	}
	if calls != 1 {
		t.Fatalf("Go calls = %d, want one failed get and no retry", calls)
	}
}

func TestResolveRemotePackageProvidersValidatesQueriesBeforeRunningGo(t *testing.T) {
	calls := 0
	runGo := func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return nil, errors.New("Go runner should not be called")
	}

	got, err := resolveRemotePackageProvidersWithRunner(t.Context(), nil, "", "", "", runGo)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty query result = %v, %v; want an empty result without error", got, err)
	}
	for _, query := range []remotePackageQuery{
		{version: "v1.2.0"},
		{packagePath: "example.invalid/repo/nexssflow"},
	} {
		if _, err := resolveRemotePackageProvidersWithRunner(t.Context(), []remotePackageQuery{query}, "", "", "", runGo); err == nil || !strings.Contains(err.Error(), "package path and explicit version are required") {
			t.Errorf("invalid query %+v error = %v, want input validation error", query, err)
		}
	}
	if calls != 0 {
		t.Fatalf("Go runner called %d times for empty or invalid queries", calls)
	}
}

func TestResolveRemotePackageProvidersRejectsInvalidGoListOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		listErr string
		wantErr string
	}{
		{name: "list command failure", listErr: "go list failed", wantErr: "go list failed"},
		{name: "malformed JSON", output: "not json", wantErr: "decode go list package metadata"},
		{name: "missing module", output: `{"ImportPath":"example.invalid/repo/nexssflow"}`, wantErr: "go list did not report a providing module"},
		{name: "missing requested package", output: `{"ImportPath":"example.invalid/other","Module":{"Path":"example.invalid/other","Version":"v1.2.0"}}`, wantErr: "go list did not report package"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runGo := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[0] == "get" {
					return nil, nil
				}
				if tc.listErr != "" {
					return nil, fmt.Errorf("%s", tc.listErr)
				}
				return []byte(tc.output), nil
			}
			_, err := resolveRemotePackageProvidersWithRunner(
				t.Context(),
				[]remotePackageQuery{{packagePath: "example.invalid/repo/nexssflow", version: "v1.2.0"}},
				"", "", "", runGo,
			)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("resolver error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
