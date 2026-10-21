package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func writeSourceGraphFixture(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessSourceGraphNestedRelativeIncludesAndConstants(t *testing.T) {
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	writeSourceGraphFixture(t, flow, "@include \"parts/child.nflow\"\nroot.marker\n")
	writeSourceGraphFixture(t, filepath.Join(root, "parts", "child.nflow"), "@const TARGET = \"deep/leaf.nflow\"\n@include \"${TARGET}\"\nchild.marker\n")
	writeSourceGraphFixture(t, filepath.Join(root, "parts", "deep", "leaf.nflow"), "leaf.marker\n")

	graph, err := harnessSourceGraph(t.Context(), flow)
	ktest.RequireNoError(t, err)
	for _, marker := range []string{"root.marker", "child.marker", "leaf.marker", "@include \"${TARGET}\""} {
		ktest.RequireStringContains(t, graph, marker)
	}
}

func TestHarnessSourceGraphDiamondDoesNotLookLikeCycle(t *testing.T) {
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	writeSourceGraphFixture(t, flow, "@include \"left/branch.nflow\"\n@include \"right/branch.nflow\"\n")
	writeSourceGraphFixture(t, filepath.Join(root, "left", "branch.nflow"), "@include \"../common/shared.nflow\"\nleft.marker\n")
	writeSourceGraphFixture(t, filepath.Join(root, "right", "branch.nflow"), "@include \"../common/shared.nflow\"\nright.marker\n")
	writeSourceGraphFixture(t, filepath.Join(root, "common", "shared.nflow"), "shared.marker\n")

	graph, err := harnessSourceGraph(t.Context(), flow)
	ktest.RequireNoError(t, err)
	for _, marker := range []string{"left.marker", "right.marker", "shared.marker"} {
		ktest.RequireStringContains(t, graph, marker)
	}
}

func TestHarnessSourceGraphReportsIncludeFailures(t *testing.T) {
	t.Run("real cycle", func(t *testing.T) {
		root := t.TempDir()
		flow := filepath.Join(root, "main.nflow")
		writeSourceGraphFixture(t, flow, "@include \"child.nflow\"\n")
		writeSourceGraphFixture(t, filepath.Join(root, "child.nflow"), "@include \"main.nflow\"\n")

		_, err := harnessSourceGraph(t.Context(), flow)
		ktest.RequireErrorContains(t, err, "cycle detected")
	})

	t.Run("missing file", func(t *testing.T) {
		root := t.TempDir()
		flow := filepath.Join(root, "main.nflow")
		writeSourceGraphFixture(t, flow, "@include \"missing/child.nflow\"\n")

		_, err := harnessSourceGraph(t.Context(), flow)
		if err == nil {
			t.Fatal("harnessSourceGraph succeeded for a missing include")
		}
	})
}

func TestHarnessSourceGraphHonorsCancelledContext(t *testing.T) {
	flow := filepath.Join(t.TempDir(), "main.nflow")
	writeSourceGraphFixture(t, flow, "root.marker\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := harnessSourceGraph(ctx, flow)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("harnessSourceGraph error = %v, want context.Canceled", err)
	}
}

func TestSourceRequiresFromRelativeIncludedChild(t *testing.T) {
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	writeSourceGraphFixture(t, flow, "@include \"sub/requirements.nflow\"\n")
	writeSourceGraphFixture(t, filepath.Join(root, "sub", "requirements.nflow"), "@require resilience\n")

	reqs, err := SourceRequires(t.Context(), flow)
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, hasRequirement(reqs, "resilience"), "relative child @require was not returned: %#v", reqs)
}

func TestStageBuildDirDetectsChildNativeBundlesAndKeepsWorkflowSource(t *testing.T) {
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	source := "@include \"child/features.nflow\"\n{} -> runtime.noop\n"
	writeSourceGraphFixture(t, flow, source)
	writeSourceGraphFixture(t, filepath.Join(root, "child", "features.nflow"), `@schema GraphChild {
  Name string
}
@pipeline child_pipeline
  secure.action :auth
  render.markdown
  stream.batch
@end
`)

	buildDir, err := stageBuildDir(t.Context(), flow, []byte(source), nil, buildOptions{})
	ktest.RequireNoError(t, err)
	defer func() { _ = os.RemoveAll(buildDir) }()

	mainSourceBytes, err := os.ReadFile(filepath.Join(buildDir, "main.go"))
	ktest.RequireNoError(t, err)
	mainSource := string(mainSourceBytes)
	for _, pkg := range []string{
		"github.com/nexssp/flow/extensions/schema",
		"github.com/nexssp/flow/extensions/pipeline",
		"github.com/nexssp/flow/extensions/modifiers_auth",
		"github.com/nexssp/flow/extensions/render",
		"github.com/nexssp/flow/extensions/stream_ops",
	} {
		if !strings.Contains(mainSource, `"`+pkg+`"`) {
			t.Errorf("generated main.go does not import child-only bundle %q:\n%s", pkg, mainSource)
		}
	}

	embedded, err := os.ReadFile(filepath.Join(buildDir, "workflow.nflow"))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(embedded), source)
}
