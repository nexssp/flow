package builder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/require"
)

func TestModuleSourceFingerprintContent(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"library.go", "go.mod", "go.sum", "assets/config.yaml", "testdata/message.txt", "vendor/example/data.txt"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(name))
			ktest.RequireNoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			ktest.RequireNoError(t, os.WriteFile(path, []byte("before"), 0o600))
			stamp := time.Unix(1_700_000_000, 0)
			ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
			before := moduleSourceFingerprint(root)
			ktest.RequireEqual(t, moduleSourceFingerprint(root), before)
			ktest.RequireNoError(t, os.WriteFile(path, []byte("after!"), 0o600))
			ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
			ktest.RequireCondition(t, moduleSourceFingerprint(root) != before, "same-size edit with preserved mtime must invalidate %s", name)
		})
	}
}

func TestModuleSourceFingerprintIgnoresMtime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "library.go")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package example"), 0o600))
	before := moduleSourceFingerprint(root)
	stamp := time.Unix(1_700_000_000, 0)
	ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
	ktest.RequireEqual(t, moduleSourceFingerprint(root), before)
}

func TestModuleSourceFingerprintFileSet(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "first.go")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package example"), 0o600))
	before := moduleSourceFingerprint(root)
	renamed := filepath.Join(root, "other.go")
	ktest.RequireNoError(t, os.Rename(path, renamed))
	ktest.RequireCondition(t, moduleSourceFingerprint(root) != before, "rename must invalidate fingerprint")
	before = moduleSourceFingerprint(root)
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package example"), 0o600))
	ktest.RequireCondition(t, moduleSourceFingerprint(root) != before, "addition must invalidate fingerprint")
	ktest.RequireNoError(t, os.Remove(path))
	ktest.RequireEqual(t, moduleSourceFingerprint(root), before)
}

func TestModuleSourceFingerprintIgnoredDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	before := moduleSourceFingerprint(root)
	for _, name := range []string{".git", "node_modules"} {
		dir := filepath.Join(root, name)
		ktest.RequireNoError(t, os.MkdirAll(dir, 0o700))
		ktest.RequireNoError(t, os.WriteFile(filepath.Join(dir, "ignored.go"), []byte("ignored"), 0o600))
	}
	ktest.RequireEqual(t, moduleSourceFingerprint(root), before)
}

func TestModuleSourceFingerprintIncompleteScanNotReusable(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	first := moduleSourceFingerprint(missing)
	second := moduleSourceFingerprint(missing)
	ktest.RequireCondition(t, first != second, "failed scan must not yield a reusable fingerprint")
}

func TestHarnessKeyLocalModuleContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "library.go")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package first"), 0o600))
	stamp := time.Unix(1_700_000_000, 0)
	ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
	reqs := []require.Requirement{{Import: "example.invalid/local", LocalPath: root}}
	before := harnessKey(reqs, "")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package other"), 0o600))
	ktest.RequireNoError(t, os.Chtimes(path, stamp, stamp))
	ktest.RequireCondition(t, harnessKey(reqs, "") != before, "local content change must invalidate runner key")
}

func TestModuleSourceFingerprintExcludesHarnessCache(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	t.Setenv(harnessCacheEnv, cache)
	before := moduleSourceFingerprint(root)
	ktest.RequireNoError(t, os.MkdirAll(cache, 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(cache, "runner"), []byte("compiled runner"), 0o600))
	ktest.RequireEqual(t, moduleSourceFingerprint(root), before)
}

func TestModuleSourceFingerprintSymlinkRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "library.go")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package first"), 0o600))
	link := filepath.Join(t.TempDir(), "source")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before := moduleSourceFingerprint(link)
	ktest.RequireEqual(t, before, moduleSourceFingerprint(root))
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package other"), 0o600))
	ktest.RequireCondition(t, moduleSourceFingerprint(link) != before, "symlinked root must track target content")
}

func TestHarnessCacheSameMtimeEmbeddedAsset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration harness build in short mode")
	}
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	t.Setenv(harnessCacheEnv, filepath.Join(root, "cache"))
	local := filepath.Join(root, "cacheedit")
	ktest.RequireNoError(t, os.MkdirAll(filepath.Join(local, "testdata"), 0o700))
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(local, "go.mod"), []byte("module example.invalid/cacheedit\ngo 1.26.9\n"), 0o600))
	library := `package cacheedit

import (
	"context"
	_ "embed"
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

//go:embed testdata/version.txt
var version string

func init() { compiler.Register("cacheedit", Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	act := action.New("cacheedit.version", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"version": version}, nil
	}).Build()
	return compiler.Bundle{ID: "cacheedit", Libraries: []action.Library{{Name: "cacheedit", Actions: []action.AnyAction{act}}}}
}
`
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(local, "library.go"), []byte(library), 0o600))
	asset := filepath.Join(local, "testdata", "version.txt")
	flow := filepath.Join(root, "main.nflow")
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
			ktest.RequireCondition(t, bin != previous, "embedded edit must choose a new runner")
		}
		cached, err := EnsureHarnessContext(t.Context(), reqs, flow)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, cached, bin)
		ktest.RequireEqual(t, ExecHarnessContext(t.Context(), bin, []string{"run", flow}), 0)
		previous = bin
	}
}

func TestModuleSourceFingerprintCacheRootEqualsSource(t *testing.T) {
	root := t.TempDir()
	t.Setenv(harnessCacheEnv, root)
	path := filepath.Join(root, "library.go")
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package first"), 0o600))
	before := moduleSourceFingerprint(root)
	ktest.RequireNoError(t, os.WriteFile(path, []byte("package other"), 0o600))
	ktest.RequireCondition(t, moduleSourceFingerprint(root) != before, "cache root must never hide source changes")
}
