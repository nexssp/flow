package data

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundleWiresDatasetFeaturesAndFixture(t *testing.T) {
	t.Parallel()
	bundle := Bundle(nil)
	ktest.RequireEqual(t, bundle.ID, ID)
	ktest.RequireEqual(t, len(bundle.Libraries), 1)
	ktest.RequireEqual(t, bundle.Libraries[0].Name, ID)
	ktest.RequireEqual(t, len(bundle.Libraries[0].Sources), 1)
	ktest.RequireEqual(t, bundle.Libraries[0].Sources[0].Describe().Name, "data.rows")
	ktest.RequireEqual(t, len(bundle.Libraries[0].Operators), 3)
	ktest.RequireCondition(t, bundle.WrapPipeline != nil, "dataset context wrapper is missing")
	ktest.RequireEqual(t, len(bundle.Directives), 2)
	ktest.RequireEqual(t, bundle.Directives[0].Name, "data")
	ktest.RequireEqual(t, bundle.Directives[1].Name, "datause")
	ktest.RequireCondition(t, bundle.InheritScope != nil, "dataset declarations are not inherited by child compilation")
	ktest.RequireCondition(t, bundle.Fixtures != nil, "embedded fixtures are missing")

	sections := bundle.AllSelfTests()
	ktest.RequireCondition(t, len(sections) > 0, "fixture was not discovered")
	found := false
	for _, section := range sections {
		for _, feature := range section.Features {
			if feature.Name == "Master example: deterministic training-data preparation from JSONL" {
				found = true
			}
		}
	}
	ktest.RequireCondition(t, found, "master example fixture was not discovered")
}

func TestEmbeddedFixturesHaveDescriptionsAndCompanionData(t *testing.T) {
	t.Parallel()
	var found bool
	err := fs.WalkDir(fixturesFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if !strings.HasSuffix(path, ".nflow") {
			return nil
		}
		found = true
		content, readErr := fs.ReadFile(fixturesFS, path)
		ktest.RequireNoError(t, readErr)
		ktest.RequireCondition(t, strings.Contains(string(content), "@description"), "fixture %q has no description", path)
		companion := strings.TrimSuffix(path, ".nflow") + "--order-cases.jsonl"
		_, companionErr := fs.Stat(fixturesFS, companion)
		ktest.RequireNoError(t, companionErr)
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, found, "no .nflow fixtures were embedded")
}
