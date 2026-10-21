package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestHarnessIncludedNativeBundles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping included-flow harness build in short mode")
	}
	root := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(root, "cache"))
	local := filepath.Join(root, "included")
	shared := filepath.Join(root, "shared")
	ktest.RequireNoError(t, os.MkdirAll(local, 0o700))
	ktest.RequireNoError(t, os.MkdirAll(shared, 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(local, "go.mod"), []byte("module example.invalid/included\ngo 1.26.9\n"), 0o600))
	library := `package included

import (
	"context"
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

func init() { compiler.Register("included", Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	act := action.New("included.echo", func(_ context.Context, _ any) (map[string]any, error) {
		return map[string]any{"value": "ok"}, nil
	}).Build()
	return compiler.Bundle{ID: "included", Libraries: []action.Library{{Name: "included", Actions: []action.AnyAction{act}}}}
}
`
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(local, "library.go"), []byte(library), 0o600))
	flow := filepath.Join(root, "main.nflow")
	ktest.RequireNoError(t, os.WriteFile(flow, []byte("@include \"shared/child.nflow\"\n"), 0o600))
	child := filepath.Join(shared, "child.nflow")
	var previous string
	for _, source := range []string{
		"@require ../included\n@description \"included action\"\n{} -> included.echo\n",
		"@require ../included\n@pipeline named\n  {} -> included.echo\n@end\n@assert: result.value == \"ok\"\n{} -> pipeline.named\n",
	} {
		ktest.RequireNoError(t, os.WriteFile(child, []byte(source), 0o600))
		reqs, err := SourceRequires(t.Context(), flow)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, len(reqs), 1)
		bin, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		if previous != "" {
			ktest.RequireCondition(t, bin != previous, "new native bundles in an included file must select another runner")
		}
		cached, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, cached, bin)
		ktest.RequireEqual(t, ExecHarnessContext(t.Context(), bin, []string{"run", flow}), 0)
		ktest.RequireEqual(t, ExecHarnessContext(t.Context(), bin, []string{"lint", flow}), 0)
		previous = bin
	}
}
