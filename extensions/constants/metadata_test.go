package constants

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestMergeConstantsMetadataCopiesAndParentWins(t *testing.T) {
	parentValues := map[string]string{"env.region": "local", "env.name": "orders"}
	includedValues := map[string]string{"env.region": "included", "env.zone": "west"}
	parent := map[string]any{"constants": parentValues}
	included := map[string]any{"constants": includedValues}

	if err := mergeConstantsMetadata(parent, included); err != nil {
		t.Fatal(err)
	}
	merged, ok := parent["constants"].(map[string]string)
	ktest.RequireCondition(t, ok, "constants metadata type: got %T", parent["constants"])
	if merged["env.region"] != "local" || merged["env.zone"] != "west" {
		t.Fatalf("unexpected merged constants: %#v", merged)
	}
	merged["env.zone"] = "changed"
	if _, exists := parentValues["env.zone"]; exists || includedValues["env.zone"] != "west" {
		t.Fatal("merged constants retained an alias to an input map")
	}
}

func TestApplyMergedConstantsSubstitutesLinesAfterInclude(t *testing.T) {
	lines := []string{"@include child.nflow", "subject: ${env.topic}", "untouched"}
	req := compiler.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   map[string]any{"constants": map[string]string{"env.topic": "orders.created"}},
	}
	if err := applyMergedConstants(req); err != nil {
		t.Fatal(err)
	}
	if lines[1] != "subject: orders.created" || lines[2] != "untouched" {
		t.Fatalf("unexpected substituted lines: %#v", lines)
	}
}
