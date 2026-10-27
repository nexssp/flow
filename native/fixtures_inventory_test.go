package native_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
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

func TestSelftest_EmbeddedNFlowFixturesCompile(t *testing.T) {
	bundles := native.SelftestBundles()
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	compiled := 0
	for _, bundle := range bundles {
		if bundle.Fixtures == nil {
			continue
		}
		for _, section := range bundle.AllSelfTests() {
			for _, feature := range section.Features {
				prefix := bundle.ID + "/"
				if !strings.HasPrefix(feature.Source, prefix) {
					continue
				}
				relativeSource := strings.TrimPrefix(feature.Source, prefix)
				if !strings.HasSuffix(relativeSource, ".nflow") || feature.Skip != "" {
					continue
				}
				directory := t.TempDir()
				file := filepath.Join(directory, filepath.FromSlash(relativeSource))
				if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
					t.Errorf("create fixture directory for %s: %v", feature.Source, err)
					continue
				}
				if err := os.WriteFile(file, []byte(feature.DSL), 0o600); err != nil {
					t.Errorf("write fixture %s: %v", feature.Source, err)
					continue
				}
				for name, content := range feature.Files {
					if !fs.ValidPath(name) {
						t.Errorf("fixture %s has invalid sidecar path %q", feature.Source, name)
						continue
					}
					target := filepath.Join(filepath.Dir(file), filepath.FromSlash(name))
					if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
						t.Errorf("create sidecar directory for %s: %v", feature.Source, err)
						continue
					}
					if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
						t.Errorf("write sidecar %s for %s: %v", name, feature.Source, err)
					}
				}
				_, compileErr := runner.Compile(t.Context(), cfg, feature.DSL, file)
				if feature.ExpectCompileError != "" {
					if compileErr == nil || !strings.Contains(compileErr.Error(), feature.ExpectCompileError) {
						t.Errorf("compile %s: got %v, want error containing %q", feature.Source, compileErr, feature.ExpectCompileError)
					}
					compiled++
					continue
				}
				if compileErr != nil {
					var skippable interface{ SelfTestSkipReason() string }
					if errors.As(compileErr, &skippable) && skippable.SelfTestSkipReason() != "" {
						t.Logf("skip %s: %s", feature.Source, skippable.SelfTestSkipReason())
						continue
					}
					t.Errorf("compile %s: %v", feature.Source, compileErr)
				}
				compiled++
			}
		}
	}
	if compiled == 0 {
		t.Fatal("no embedded .nflow fixtures were compiled")
	}
}
