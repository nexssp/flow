package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"
)

func TestSourceMapLoadResolvesRelativeAndAbsolutePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "root.nflow")
	nested := filepath.Join(dir, "parts", "nested.nflow")
	shared := filepath.Join(dir, "shared.nflow")
	sources := SourceMap{
		filepath.Clean(shared): "shared source",
	}

	data, resolved, err := sources.Load(nested, "../shared.nflow")
	if err != nil {
		t.Fatalf("Load relative path: %v", err)
	}
	if string(data) != "shared source" || resolved != filepath.Clean(shared) {
		t.Fatalf("Load relative path = (%q, %q), want (%q, %q)", data, resolved, "shared source", filepath.Clean(shared))
	}

	data, resolved, err = sources.Load(root, shared)
	if err != nil {
		t.Fatalf("Load absolute path: %v", err)
	}
	if string(data) != "shared source" || resolved != filepath.Clean(shared) {
		t.Fatalf("Load absolute path = (%q, %q), want (%q, %q)", data, resolved, "shared source", filepath.Clean(shared))
	}
}

func TestSourceMapLoadMissingDoesNotFallBackToFilesystem(t *testing.T) {
	t.Parallel()

	physicalFile := filepath.Join(t.TempDir(), "exists.nflow")
	if err := os.WriteFile(physicalFile, []byte("on disk"), 0o600); err != nil {
		t.Fatalf("write physical file: %v", err)
	}

	_, _, err := (SourceMap{}).Load(filepath.Join(filepath.Dir(physicalFile), "root.nflow"), filepath.Base(physicalFile))
	if err == nil {
		t.Fatal("Load unexpectedly read a file absent from the snapshot")
	}
	if kind := xerr.KindFrom(err); kind != xerr.KindNotFound {
		t.Fatalf("error kind = %v, want %v (error: %v)", kind, xerr.KindNotFound, err)
	}
}

func TestSourceMapLoadReturnsIndependentBytes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "source.nflow")
	sources := SourceMap{filepath.Clean(path): "original"}
	data, _, err := sources.Load(filepath.Join(filepath.Dir(path), "root.nflow"), filepath.Base(path))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	data[0] = 'X'

	again, _, err := sources.Load("", path)
	if err != nil {
		t.Fatalf("Load after mutating returned bytes: %v", err)
	}
	if string(again) != "original" || sources[filepath.Clean(path)] != "original" {
		t.Fatalf("mutating returned bytes changed snapshot: bytes=%q stored=%q", again, sources[filepath.Clean(path)])
	}
}

func TestSourceMapStableIdentityAllowsDiamondIncludes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "root.nflow")
	common := filepath.Join(dir, "shared", "common.nflow")
	sources := SourceMap{
		filepath.Clean(filepath.Join(dir, "left", "left.nflow")):   "@test_include ../shared/common.nflow\nleft\n",
		filepath.Clean(filepath.Join(dir, "right", "right.nflow")): "@test_include ../shared/./common.nflow\nright\n",
		filepath.Clean(common): "common\n",
	}

	leftData, leftID, err := sources.Load(filepath.Join(dir, "left", "left.nflow"), "../shared/common.nflow")
	if err != nil {
		t.Fatalf("Load common from left: %v", err)
	}
	rightData, rightID, err := sources.Load(filepath.Join(dir, "right", "right.nflow"), "../shared/./common.nflow")
	if err != nil {
		t.Fatalf("Load common from right: %v", err)
	}
	if leftID != rightID || !bytes.Equal(leftData, rightData) {
		t.Fatalf("same snapshot source got different identities/content: left=(%q,%q), right=(%q,%q)", leftID, leftData, rightID, rightData)
	}

	ctx := WithSourceLoader(t.Context(), sources)
	clean, _, err := Preprocess(ctx, NewDirectiveTable(testIncludeDirective()), "@test_include left/left.nflow\n@test_include right/right.nflow", root)
	if err != nil {
		t.Fatalf("Preprocess diamond: %v", err)
	}
	if got := strings.Count(clean, "common"); got != 2 {
		t.Fatalf("common included %d times in diamond output %q, want 2", got, clean)
	}
}

func TestSourceMapStableIdentityDetectsCycleThroughPathAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "root.nflow")
	child := filepath.Join(dir, "child.nflow")
	sources := SourceMap{
		filepath.Clean(child): "@test_include ./folder/../child.nflow\n",
	}

	ctx := WithSourceLoader(t.Context(), sources)
	_, _, err := Preprocess(ctx, NewDirectiveTable(testIncludeDirective()), "@test_include child.nflow", root)
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("Preprocess error = %v, want cycle detected for path alias", err)
	}
}
