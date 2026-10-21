package require

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMetadataMergerRejectsAliasCollisionAcrossFiles(t *testing.T) {
	t.Parallel()
	parent := map[string]any{"require": []Requirement{{Import: "example.com/first", Alias: "shared", Options: map[string]string{"id": "parent"}}}}
	included := map[string]any{"require": []Requirement{{Import: "example.com/second", Alias: "shared"}}}

	err := Directive.Metadata.Merge(parent, included)
	ktest.RequireErrorContains(t, err, `namespace qualifier "shared" is already used`)
	reqs := FromMeta(parent)
	ktest.RequireEqual(t, len(reqs), 1)
	ktest.RequireEqual(t, reqs[0].Options["id"], "parent")
}

func TestMetadataMergerDeduplicatesSameRequirementWithoutDroppingOptions(t *testing.T) {
	t.Parallel()
	parent := map[string]any{"require": []Requirement{{Import: "example.com/pkg", Version: "v1.0.0", Alias: "pkg", Options: map[string]string{"id": "parent"}}}}
	included := map[string]any{"require": []Requirement{{Import: "example.com/pkg", Version: "v1.0.0", Alias: "pkg", Options: map[string]string{"id": "included"}}}}

	ktest.RequireNoError(t, Directive.Metadata.Merge(parent, included))
	reqs := FromMeta(parent)
	ktest.RequireEqual(t, len(reqs), 1)
	ktest.RequireEqual(t, reqs[0].Options["id"], "parent")
	ktest.RequireEqual(t, len(FromMeta(included)), 1)
}
