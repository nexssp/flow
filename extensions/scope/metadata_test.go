package scope

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestProfilesMetadataMergerUnionsWithoutMutatingInputs(t *testing.T) {
	t.Parallel()
	parentProfiles := map[string]profileDef{"base": {mods: []string{":retry=2"}}}
	includedProfiles := map[string]profileDef{"fast": {parent: "base", mods: []string{":timeout=1s"}}}
	parent := map[string]any{profilesKey: parentProfiles}
	included := map[string]any{profilesKey: includedProfiles}

	ktest.RequireNoError(t, ProfileDirective.Metadata.Merge(parent, included))
	merged := profilesFromMeta(parent)
	ktest.RequireEqual(t, len(merged), 2)
	ktest.RequireEqual(t, merged["fast"].parent, "base")
	ktest.RequireEqual(t, len(parentProfiles), 1)
	ktest.RequireEqual(t, len(includedProfiles), 1)
	mods, err := ExpandProfile(parent, "fast")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, mods, []string{":retry=2", ":timeout=1s"})
}

func TestProfilesMetadataMergerRejectsDuplicateName(t *testing.T) {
	t.Parallel()
	parent := map[string]any{profilesKey: map[string]profileDef{"same": {mods: []string{":retry=1"}}}}
	included := map[string]any{profilesKey: map[string]profileDef{"same": {mods: []string{":retry=2"}}}}
	ktest.RequireErrorContains(t, ProfileDirective.Metadata.Merge(parent, included), "duplicate declaration")
	ktest.RequireEqual(t, len(profilesFromMeta(parent)), 1)
}
