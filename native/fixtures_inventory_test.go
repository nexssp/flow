package native_test

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/native"
)

// Compare every file, including sidecars, rather than only checking bundles
// already present in SelftestBundles. The latter cannot catch an unregistered
// extension or a fixture accidentally placed outside its owning package.
func TestSelftest_RepositoryFixturesMatchEmbeddedFiles(t *testing.T) {
	embedded := make(map[string][]byte)
	// @embed requires a host-declared filesystem root, so its real NFlow
	// fixture is embedded and executed by embed.TestHostRootNFlowFixture,
	// not the stock native bundle set. Keep this exception file-exact so
	// additional host fixtures cannot silently escape a coverage decision.
	hostFixtures := map[string]bool{
		"embed/nflows/host_root.nflow":      false,
		"embed/nflows/host_root--terms.txt": false,
	}
	for _, bundle := range native.SelftestBundles() {
		ktest.RequireCondition(t, bundle.Fixtures != nil, "bundle %q has no fixtures", bundle.ID)
		err := fs.WalkDir(bundle.Fixtures, ".", func(p string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			key := path.Join(bundle.ID, p)
			ktest.RequireCondition(t, embedded[key] == nil, "duplicate embedded fixture %s", key)
			data, err := fs.ReadFile(bundle.Fixtures, p)
			if err != nil {
				return err
			}
			embedded[key] = data
			return nil
		})
		ktest.RequireNoError(t, err)
	}

	root := filepath.Join("..", "extensions")
	rootFS, err := os.OpenRoot(root)
	ktest.RequireNoError(t, err)
	t.Cleanup(func() { ktest.RequireNoError(t, rootFS.Close()) })
	count := 0
	err = filepath.WalkDir(root, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.Contains(key, "/nflows/") {
			return nil
		}
		count++
		data, err := rootFS.ReadFile(rel)
		if err != nil {
			return err
		}
		if _, hostConfigured := hostFixtures[key]; hostConfigured {
			ktest.RequireCondition(t, len(data) > 0, "empty host fixture %s", key)
			hostFixtures[key] = true
			return nil
		}
		got, ok := embedded[key]
		ktest.RequireCondition(t, ok, "repository fixture %s is not embedded by a self-test bundle", key)
		ktest.RequireCondition(t, bytes.Equal(got, data), "embedded fixture %s differs from the repository", key)
		delete(embedded, key)
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, count > 0, "repository fixture scan found no files")
	for key, found := range hostFixtures {
		ktest.RequireCondition(t, found, "host-configured fixture %s is missing", key)
	}
	for key := range embedded {
		t.Errorf("embedded fixture %s has no matching repository file", key)
	}
}
