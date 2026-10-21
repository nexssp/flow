package builder

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
)

func TestPackagedSourcesPreserveIncludeInputsAndRewriteChildRequires(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	child := filepath.Join(root, "parts", "child.nflow")
	local := filepath.Join(root, "local")
	writeSourceGraphFixture(t, flow, "@include parts/child.nflow\n")
	writeSourceGraphFixture(t, child, "@require ../local\n@const ACTION = \"runtime.noop\"\n@pipeline named\n{} -> ${ACTION}\n@end\n")
	writeSourceGraphFixture(t, filepath.Join(local, "go.mod"), "module example.invalid/local\ngo 1.26.9\n")
	writeSourceGraphFixture(t, filepath.Join(local, "library.go"), "package local\n")
	reqs := []require.Requirement{{Import: "example.invalid/local", LocalPath: local, ModuleRoot: local}}
	sources, files, err := packageBuildSources(t.Context(), flow, "@include parts/child.nflow\n", reqs)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(sources), 2)
	ktest.RequireEqual(t, len(files), 2)
	ktest.RequireStringContains(t, files[child], "@require example.invalid/local")
	ktest.RequireStringContains(t, files[child], "${ACTION}")
	ktest.RequireStringContains(t, files[flow], "@include parts/child.nflow")
	serialized, err := json.Marshal(files)
	ktest.RequireNoError(t, err)
	var packaged compiler.SourceMap
	ktest.RequireNoError(t, json.Unmarshal(serialized, &packaged))
	data, name, err := packaged.Load(flow, "parts/child.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, name, child)
	ktest.RequireEqual(t, string(data), files[child])
}

func TestBuildStandaloneIncludesWithoutOriginalSources(t *testing.T) {
	if testing.Short() {
		t.Skip("standalone build integration")
	}
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	ktest.RequireNoError(t, os.CopyFS(root, os.DirFS("testdata/include-package")))
	binary := filepath.Join(t.TempDir(), "included-flow"+exeSuffix())
	ktest.RequireEqual(t, runBuild([]string{flow, "-o", binary}), 0)
	ktest.RequireNoError(t, os.RemoveAll(root))
	command := exec.CommandContext(t.Context(), binary, "--json")
	command.Dir = t.TempDir()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("standalone include without original sources: %v\n%s", err, output)
	}
	ktest.RequireStringContains(t, string(output), `"value":"two  spaces"`)
}
