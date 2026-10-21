package pipeline

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestMergePipelineMetadataAlignmentErrorIsCopyOnWrite(t *testing.T) {
	parentModifiers := map[string][]string{"ship": {"timeout=2s"}, "keep": {"retry=3"}}
	parentSources := map[string][]compiler.ModifierSource{"ship": {{Kind: "pipeline", Label: "old"}}, "keep": {{Kind: "profile", Label: "base"}}}
	parent := map[string]any{
		"pipelines":                 map[string]string{"ship": "old body"},
		"pipeline_modifiers":        parentModifiers,
		"pipeline_modifier_sources": parentSources,
	}
	includedModifiers := map[string][]string{"ship": {"timeout=1s", "retry=2"}}
	includedSources := map[string][]compiler.ModifierSource{"ship": {{Kind: "pipeline", Label: "incoming"}}}
	included := map[string]any{
		"pipelines":                 map[string]string{"ship": "new body"},
		"pipeline_modifiers":        includedModifiers,
		"pipeline_modifier_sources": includedSources,
	}

	err := mergePipelineMetadata(parent, included)
	ktest.RequireCondition(t, err != nil && strings.Contains(err.Error(), "2 modifiers but 1 modifier sources"), "expected alignment error, got %v", err)
	pipelines, ok := parent["pipelines"].(map[string]string)
	ktest.RequireCondition(t, ok, "pipelines metadata type: got %T", parent["pipelines"])
	if pipelines["ship"] != "old body" || parentModifiers["ship"][0] != "timeout=2s" || parentSources["ship"][0].Label != "old" {
		t.Fatal("failed merge mutated parent metadata")
	}
	if includedModifiers["ship"][0] != "timeout=1s" || includedSources["ship"][0].Label != "incoming" {
		t.Fatal("failed merge mutated included metadata")
	}
}

func TestMergePipelineMetadataReplacementDropsStaleModifiersAndSources(t *testing.T) {
	oldModifiers := map[string][]string{"replace": {"retry=2"}, "keep": {"dedup"}}
	oldSources := map[string][]compiler.ModifierSource{"replace": {{Kind: "profile", Label: "old"}}, "keep": {{Kind: "pipeline", Label: "keep"}}}
	parent := map[string]any{
		"pipelines":                 map[string]string{"replace": "old", "keep": "keep body"},
		"pipeline_modifiers":        oldModifiers,
		"pipeline_modifier_sources": oldSources,
	}
	included := map[string]any{"pipelines": map[string]string{"replace": "new"}}

	if err := mergePipelineMetadata(parent, included); err != nil {
		t.Fatal(err)
	}
	pipelines, ok := parent["pipelines"].(map[string]string)
	ktest.RequireCondition(t, ok, "pipelines metadata type: got %T", parent["pipelines"])
	if pipelines["replace"] != "new" {
		t.Fatal("replacement pipeline body was not installed")
	}
	modifiers, ok := parent["pipeline_modifiers"].(map[string][]string)
	ktest.RequireCondition(t, ok, "pipeline modifiers metadata type: got %T", parent["pipeline_modifiers"])
	if _, exists := modifiers["replace"]; exists {
		t.Fatal("stale modifiers remained on replacement pipeline")
	}
	sources, ok := parent["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)
	ktest.RequireCondition(t, ok, "pipeline modifier sources metadata type: got %T", parent["pipeline_modifier_sources"])
	if _, exists := sources["replace"]; exists {
		t.Fatal("stale modifier sources remained on replacement pipeline")
	}
	if oldModifiers["replace"][0] != "retry=2" || oldSources["replace"][0].Label != "old" {
		t.Fatal("successful merge mutated original parent slices")
	}
	modifiers, ok = parent["pipeline_modifiers"].(map[string][]string)
	ktest.RequireCondition(t, ok, "pipeline modifiers metadata type: got %T", parent["pipeline_modifiers"])
	if modifiers["keep"][0] != "dedup" {
		t.Fatal("unrelated pipeline metadata was lost")
	}
}
