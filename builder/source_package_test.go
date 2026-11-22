package builder

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	ktest.RequireStringContains(t, files["parts/child.nflow"], "@require example.invalid/local")
	ktest.RequireStringContains(t, files["parts/child.nflow"], "${ACTION}")
	ktest.RequireStringContains(t, files["main.nflow"], "@include parts/child.nflow")
	serialized, err := json.Marshal(files)
	ktest.RequireNoError(t, err)
	if strings.Contains(string(serialized), root) {
		t.Fatalf("source manifest contains an absolute temp path: %s", serialized)
	}
	var packaged compiler.SourceMap
	ktest.RequireNoError(t, json.Unmarshal(serialized, &packaged))
	data, name, err := packaged.Load("main.nflow", "parts/child.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, name, "parts/child.nflow")
	ktest.RequireEqual(t, string(data), files["parts/child.nflow"])
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

func TestPackagedSourcesRewriteAbsoluteIncludesToRelativePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	child := filepath.Join(root, "parts", "child.nflow")
	source := `@include "` + child + `"` + "\n"
	writeSourceGraphFixture(t, flow, source)
	writeSourceGraphFixture(t, child, "child.marker\n")

	_, files, err := packageBuildSources(t.Context(), flow, source, nil)
	ktest.RequireNoError(t, err)
	if strings.Contains(files["main.nflow"], root) {
		t.Fatalf("packaged root source retains its absolute include path: %s", files["main.nflow"])
	}
	ktest.RequireStringContains(t, files["main.nflow"], `@include "parts/child.nflow"`)
	if _, ok := files["parts/child.nflow"]; !ok {
		t.Fatalf("relative child source missing from packaged files: %#v", files)
	}
	serialized, err := json.Marshal(files)
	ktest.RequireNoError(t, err)
	if strings.Contains(string(serialized), root) {
		t.Fatalf("source manifest contains an absolute temp path: %s", serialized)
	}

	var packaged compiler.SourceMap
	ktest.RequireNoError(t, json.Unmarshal(serialized, &packaged))
	data, resolved, err := packaged.Load("main.nflow", "parts/child.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, resolved, "parts/child.nflow")
	ktest.RequireEqual(t, string(data), files["parts/child.nflow"])
}

func TestPackagedSourcesRejectParentDirectoryInclude(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	root := filepath.Join(workspace, "project")
	flow := filepath.Join(root, "main.nflow")
	source := "@include ../private.nflow\n"
	writeSourceGraphFixture(t, flow, source)
	writeSourceGraphFixture(t, filepath.Join(workspace, "private.nflow"), "sensitive.marker\n")

	_, _, err := packageBuildSources(t.Context(), flow, source, nil)
	if err == nil || !strings.Contains(err.Error(), "outside the build root") {
		t.Fatalf("packageBuildSources error = %v, want outside-root rejection", err)
	}
}

func TestPackagedSourcesRejectAbsoluteIncludeOutsideRoot(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	root := filepath.Join(workspace, "project")
	flow := filepath.Join(root, "main.nflow")
	outside := filepath.Join(workspace, "private.nflow")
	source := "@include \"" + filepath.ToSlash(outside) + "\"\n"
	writeSourceGraphFixture(t, flow, source)
	writeSourceGraphFixture(t, outside, "sensitive.marker\n")

	_, _, err := packageBuildSources(t.Context(), flow, source, nil)
	if err == nil || !strings.Contains(err.Error(), "outside the build root") {
		t.Fatalf("packageBuildSources error = %v, want outside-root rejection", err)
	}
}

func TestPackagedSourcesAllowParentRelativeIncludeWithinRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	child := filepath.Join(root, "parts", "child.nflow")
	source := "@include parts/child.nflow\n"
	writeSourceGraphFixture(t, flow, source)
	writeSourceGraphFixture(t, child, "@include ../shared.nflow\n")
	writeSourceGraphFixture(t, filepath.Join(root, "shared.nflow"), "inside.marker\n")

	_, files, err := packageBuildSources(t.Context(), flow, source, nil)
	ktest.RequireNoError(t, err)
	if _, ok := files["shared.nflow"]; !ok {
		t.Fatalf("parent-relative in-root include missing from packaged files: %#v", files)
	}
}

func TestPackagedSourcesRejectOutsideSymlinkToInsideRoot(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	root := filepath.Join(workspace, "project")
	flow := filepath.Join(root, "main.nflow")
	target := filepath.Join(root, "shared.nflow")
	alias := filepath.Join(workspace, "shared-alias.nflow")
	source := "@include \"" + filepath.ToSlash(alias) + "\"\n"
	writeSourceGraphFixture(t, flow, source)
	writeSourceGraphFixture(t, target, "inside.marker\n")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, _, err := packageBuildSources(t.Context(), flow, source, nil)
	if err == nil || !strings.Contains(err.Error(), "outside the build root") {
		t.Fatalf("packageBuildSources error = %v, want lexical outside-root rejection", err)
	}
}

func TestPackagedSourcesRejectSymlinkEscape(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	root := filepath.Join(workspace, "project")
	flow := filepath.Join(root, "main.nflow")
	source := "@include linked.nflow\n"
	writeSourceGraphFixture(t, flow, source)
	outside := filepath.Join(workspace, "private.nflow")
	writeSourceGraphFixture(t, outside, "sensitive.marker\n")
	if err := os.Symlink(outside, filepath.Join(root, "linked.nflow")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, _, err := packageBuildSources(t.Context(), flow, source, nil)
	if err == nil || !strings.Contains(err.Error(), "outside the build root") {
		t.Fatalf("packageBuildSources error = %v, want symlink-escape rejection", err)
	}
}
