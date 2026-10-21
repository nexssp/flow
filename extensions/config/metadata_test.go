package config

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestConfigMetadataCombinesIncludeDefaultsWithoutAliasing(t *testing.T) {
	t.Parallel()
	local := map[string]string{"region": "local", "name": "orders"}
	defaults := map[string]string{"region": "child", "zone": "west"}
	parent := map[string]any{"config": local}
	ktest.RequireNoError(t, mergeConfigMetadata(parent, map[string]any{"config": defaults}))
	merged, err := configMetadataValue(parent)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, merged, map[string]string{"region": "local", "name": "orders", "zone": "west"})
	merged["zone"] = "changed"
	ktest.RequireEqual(t, defaults["zone"], "west")
	_, exists := local["zone"]
	ktest.RequireFalse(t, exists)
}

func TestConfigMetadataRejectsWrongType(t *testing.T) {
	t.Parallel()
	ktest.RequireErrorContains(t, mergeConfigMetadata(map[string]any{}, map[string]any{"config": []string{"wrong"}}), "config metadata")
}
