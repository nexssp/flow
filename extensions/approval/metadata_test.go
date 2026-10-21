package approval

import (
	"strings"
	"testing"
	"time"

	"github.com/nexssp/flow/compiler"
)

func TestMergeApprovalMetadataDuplicateUsesIncomingPositionAndPreservesInputs(t *testing.T) {
	parentDeclarations := []Declaration{{
		Name: "finance_review", Role: "reviewer", Reason: "existing", Expires: time.Minute,
		Position: compiler.Position{File: "parent.nflow", Line: 4},
	}}
	includedDeclarations := []Declaration{{
		Name: "finance_review", Role: "reviewer", Reason: "included", Expires: time.Minute,
		Position: compiler.Position{File: "included.nflow", Line: 9},
	}}
	parent := map[string]any{metadataKey: parentDeclarations}
	included := map[string]any{metadataKey: includedDeclarations}

	err := mergeApprovalMetadata(parent, included)
	if err == nil || !strings.Contains(err.Error(), "already at parent.nflow:4") {
		t.Fatalf("expected duplicate error with existing position, got %v", err)
	}
	if pos, ok := compiler.PositionOf(err); !ok || pos != includedDeclarations[0].Position {
		t.Fatalf("expected incoming source position, got %v, %v", pos, ok)
	}
	if len(DeclarationsFromMeta(parent)) != 1 || len(parentDeclarations) != 1 || len(includedDeclarations) != 1 {
		t.Fatal("failed merge changed approval input slices")
	}
}

func TestMergeApprovalMetadataRejectsTypeMismatch(t *testing.T) {
	parent := map[string]any{metadataKey: []string{"not an approval declaration"}}
	included := map[string]any{metadataKey: []Declaration{{Name: "next"}}}
	if err := mergeApprovalMetadata(parent, included); err == nil || !strings.Contains(err.Error(), "want []approval.Declaration") {
		t.Fatalf("expected metadata type error, got %v", err)
	}
}
