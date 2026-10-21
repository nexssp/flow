package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestHarnessKeyWorkspaceReplaceContent(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"relative", "absolute", "versioned"} {
		t.Run(style, func(t *testing.T) {
			t.Parallel()
			for _, name := range []string{"value.go", "go.mod", "go.sum", "testdata/value.txt"} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					root := t.TempDir()
					workspace := filepath.Join(root, "workspace")
					target := filepath.Join(root, "replacement")
					ktest.RequireNoError(t, os.MkdirAll(filepath.Join(workspace, "app"), 0o700))
					ktest.RequireNoError(t, os.WriteFile(filepath.Join(workspace, "app", "go.mod"), []byte("module example.invalid/app\ngo 1.26.9\n"), 0o600))
					path := filepath.Join(target, filepath.FromSlash(name))
					ktest.RequireNoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
					ktest.RequireNoError(t, os.WriteFile(path, []byte("before"), 0o600))
					stamp := time.Unix(1_700_000_000, 0)
					ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
					directive := "replace example.invalid/dependency => ../replacement\n"
					switch style {
					case "absolute":
						directive = fmt.Sprintf("replace (\n example.invalid/dependency => %s\n)\n", filepath.ToSlash(target))
					case "versioned":
						directive = "replace example.invalid/dependency v1.0.0 => ../replacement\n"
					}
					work := filepath.Join(workspace, "go.work")
					ktest.RequireNoError(t, os.WriteFile(work, []byte("go 1.26.9\nuse ./app\n"+directive), 0o600))
					before := harnessKey(nil, work)
					ktest.RequireEqual(t, harnessKey(nil, work), before)
					ktest.RequireNoError(t, os.WriteFile(path, []byte("after!"), 0o600))
					ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
					ktest.RequireCondition(t, harnessKey(nil, work) != before, "replace-only %s edit with unchanged size and mtime must invalidate runner key", name)
				})
			}
		})
	}
}

func TestHarnessKeyWorkspaceSharedTargetsStable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "dependency")
	ktest.RequireNoError(t, os.MkdirAll(target, 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(target, "go.mod"), []byte("module example.invalid/dependency\ngo 1.26.9\n"), 0o600))
	work := filepath.Join(root, "go.work")
	contents := "go 1.26.9\nuse ./dependency\nreplace (\n example.invalid/first => ./dependency\n example.invalid/second => ./dependency\n)\n"
	ktest.RequireNoError(t, os.WriteFile(work, []byte(contents), 0o600))
	before := harnessKey(nil, work)
	for range 10 {
		ktest.RequireEqual(t, harnessKey(nil, work), before)
	}
}

func TestHarnessCacheWorkspaceReplaceSameMtimeAsset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration harness build in short mode")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	local := filepath.Join(workspace, "cacheedit")
	dependency := filepath.Join(root, "dependency")
	t.Setenv(harnessCacheEnv, filepath.Join(root, "cache"))
	t.Setenv("GOWORK", filepath.Join(workspace, "go.work"))
	ktest.RequireNoError(t, os.MkdirAll(local, 0o700))
	ktest.RequireNoError(t, os.MkdirAll(filepath.Join(dependency, "testdata"), 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(local, "go.mod"), []byte("module example.invalid/cacheedit\ngo 1.26.9\nrequire example.invalid/value v0.0.0\n"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(dependency, "go.mod"), []byte("module example.invalid/value\ngo 1.26.9\n"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(workspace, "go.work"), []byte("go 1.26.9\nuse ./cacheedit\nreplace example.invalid/value => ../dependency\n"), 0o600))
	valueSource := `package value

import _ "embed"

//go:embed testdata/version.txt
var Version string
`
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(dependency, "value.go"), []byte(valueSource), 0o600))
	library := `package cacheedit

import (
	"context"
	"example.invalid/value"
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

func init() { compiler.Register("cacheedit", Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	act := action.New("cacheedit.version", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"version": value.Version}, nil
	}).Build()
	return compiler.Bundle{ID: "cacheedit", Libraries: []action.Library{{Name: "cacheedit", Actions: []action.AnyAction{act}}}}
}
`
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(local, "library.go"), []byte(library), 0o600))
	asset := filepath.Join(dependency, "testdata", "version.txt")
	flow := filepath.Join(workspace, "main.nflow")
	stamp := time.Unix(1_700_000_000, 0)
	var previous string
	for _, version := range []string{"v1", "v2"} {
		ktest.RequireNoError(t, os.WriteFile(asset, []byte(version), 0o600))
		ktest.RequireNoError(t, os.Chtimes(asset, stamp, stamp))
		source := "@require ./cacheedit\n@assert: result.version == \"" + version + "\"\n{} -> cacheedit.version\n"
		ktest.RequireNoError(t, os.WriteFile(flow, []byte(source), 0o600))
		reqs, err := SourceRequires(t.Context(), flow)
		ktest.RequireNoError(t, err)
		bin, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		if previous != "" {
			ktest.RequireCondition(t, bin != previous, "replacement-only embedded edit must choose a new runner")
		}
		cached, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, cached, bin)
		ktest.RequireEqual(t, ExecHarnessContext(t.Context(), bin, []string{"run", flow}), 0)
		previous = bin
	}
}

func TestHarnessKeyWorkspaceModuleReplacementStable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	app := filepath.Join(root, "app")
	ktest.RequireNoError(t, os.MkdirAll(app, 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(app, "go.mod"), []byte("module example.invalid/app\ngo 1.26.9\n"), 0o600))
	work := filepath.Join(root, "go.work")
	contents := "go 1.26.9\nuse ./app\nreplace example.invalid/old v1.0.0 => example.invalid/new v1.2.3\n"
	ktest.RequireNoError(t, os.WriteFile(work, []byte(contents), 0o600))
	ktest.RequireEqual(t, len(parseGoWork(work).localReplaceDirs), 0)
	before := harnessKey(nil, work)
	for range 10 {
		ktest.RequireEqual(t, harnessKey(nil, work), before)
	}
}

func TestHarnessKeyWorkspaceVersionedReplaceTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var paths []string
	for _, name := range []string{"first", "second"} {
		path := filepath.Join(root, name, "value.go")
		ktest.RequireNoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		ktest.RequireNoError(t, os.WriteFile(path, []byte("package first"), 0o600))
		paths = append(paths, path)
	}
	work := filepath.Join(root, "go.work")
	contents := "go 1.26.9\nreplace (\n example.invalid/dependency v1.0.0 => ./first\n example.invalid/dependency v1.1.0 => ./second\n)\n"
	ktest.RequireNoError(t, os.WriteFile(work, []byte(contents), 0o600))
	for _, path := range paths {
		before := harnessKey(nil, work)
		ktest.RequireNoError(t, os.WriteFile(path, []byte("package other"), 0o600))
		ktest.RequireCondition(t, harnessKey(nil, work) != before, "each version-specific local replacement must be fingerprinted")
	}
}
