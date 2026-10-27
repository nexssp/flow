package builder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/require"
)

func TestHarnessKeyLocalSubpackageTracksModuleInputs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"shared/version.go", "go.mod", "go.sum"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			local := filepath.Join(root, "adapters", "echo")
			ktest.RequireNoError(t, os.MkdirAll(local, 0o700))
			ktest.RequireNoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/cachemodule\ngo 1.26.9\n"), 0o600))
			r, err := require.Parse("./adapters/echo", root, nil, "main.nflow", 1)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, r.LocalPath, local)
			ktest.RequireEqual(t, r.ModuleRoot, root)

			path := filepath.Join(root, filepath.FromSlash(name))
			ktest.RequireNoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			ktest.RequireNoError(t, os.WriteFile(path, []byte("before"), 0o600))
			stamp := time.Unix(1_700_000_000, 0)
			ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
			reqs := []require.Requirement{r}
			before := harnessKey(reqs, "")
			ktest.RequireEqual(t, harnessKey(reqs, ""), before)
			ktest.RequireNoError(t, os.WriteFile(path, []byte("after!"), 0o600))
			ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
			ktest.RequireCondition(t, harnessKey(reqs, "") != before, "module input outside required subpackage must invalidate runner: %s", name)
		})
	}
}

func TestHarnessKeyUsesSelectedNestedModuleRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	adapter := filepath.Join(root, "nexssflow")
	ktest.RequireNoError(t, os.MkdirAll(adapter, 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/parent\ngo 1.26.9\n"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(adapter, "go.mod"), []byte("module example.invalid/adapter\ngo 1.26.9\n"), 0o600))
	path := filepath.Join(adapter, "library.go")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package adapter"), 0o600))
	r, err := require.Parse("./", root, nil, "main.nflow", 1)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, r.ModuleRoot, adapter)
	reqs := []require.Requirement{r}
	before := harnessKey(reqs, "")
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("not in the selected module"), 0o600))
	ktest.RequireEqual(t, harnessKey(reqs, ""), before)
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package changed"), 0o600))
	ktest.RequireCondition(t, harnessKey(reqs, "") != before, "selected nested module changes must invalidate runner")
}

func TestHarnessCacheLocalSubpackageSiblingEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local subpackage harness build in short mode")
	}
	root := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(root, "cache"))
	module := filepath.Join(root, "cachemodule")
	local := filepath.Join(module, "adapters", "echo")
	shared := filepath.Join(module, "shared")
	ktest.RequireNoError(t, os.MkdirAll(local, 0o700))
	ktest.RequireNoError(t, os.MkdirAll(shared, 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.invalid/cachemodule\ngo 1.26.9\n"), 0o600))
	library := `package echo

import (
	"context"
	"example.invalid/cachemodule/shared"
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

func init() { compiler.Register("modulecache", Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	act := action.New("modulecache.version", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"version": shared.Version}, nil
	}).Build()
	return compiler.Bundle{ID: "modulecache", Libraries: []action.Library{{Name: "modulecache", Actions: []action.AnyAction{act}}}}
}
`
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(local, "library.go"), []byte(library), 0o600))
	path := filepath.Join(shared, "version.go")
	flow := filepath.Join(root, "main.nflow")
	stamp := time.Unix(1_700_000_000, 0)
	var previous string
	for _, version := range []string{"v1", "v2"} {
		ktest.RequireNoError(t, os.WriteFile(path, []byte("package shared\nconst Version = \""+version+"\"\n"), 0o600))
		ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
		source := "@require ./cachemodule/adapters/echo\n@assert: result.version == \"" + version + "\"\n{} -> modulecache.version\n"
		ktest.RequireNoError(t, os.WriteFile(flow, []byte(source), 0o600))
		reqs, err := SourceRequires(t.Context(), flow)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, len(reqs), 1)
		ktest.RequireEqual(t, reqs[0].ModuleRoot, module)
		bin, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		if previous != "" {
			ktest.RequireCondition(t, bin != previous, "sibling edit must select a newly compiled runner")
		}
		cached, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, cached, bin)
		ktest.RequireEqual(t, ExecHarnessContext(t.Context(), bin, []string{"run", flow}), 0)
		lintBin, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, ExecHarnessContext(t.Context(), lintBin, []string{"lint", flow}), 0)
		previous = bin
	}
}
