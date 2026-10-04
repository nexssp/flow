package cli

import (
	"os"
	"path/filepath"
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
	cases := []struct {
		name       string
		importPath string
		want       string
	}{
		{"DefaultModuleRoot_AppendsNexssflow", "github.com/nexssp/sandbox", "github.com/nexssp/sandbox/nexssflow"},
		{"ExplicitDefault_PreservesNexssflow", "github.com/nexssp/sandbox/nexssflow", "github.com/nexssp/sandbox/nexssflow"},
		{"CustomFolder_PreservesDevVariant", "github.com/nexssp/sandbox/nexssflow_dev", "github.com/nexssp/sandbox/nexssflow_dev"},
		{"CustomFolder_PreservesProdVariant", "github.com/nexssp/sandbox/nexssflow_prod", "github.com/nexssp/sandbox/nexssflow_prod"},
	}

	for i := range cases {
		tc := cases[i]
		t.Run(tc.name, func(t *testing.T) {
			req := require.Requirement{Import: tc.importPath}
			got := bundleImportPath(req)
			if got != tc.want {
				t.Fatalf("bundleImportPath(%q) = %q, want %q", tc.importPath, got, tc.want)
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
