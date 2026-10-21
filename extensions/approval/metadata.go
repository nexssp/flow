package approval

import (
	"fmt"

	"github.com/nexssp/flow/compiler"
)

func approvalMetadata() *compiler.MetadataMerger {
	return &compiler.MetadataMerger{
		Keys:  []string{metadataKey},
		Merge: mergeApprovalMetadata,
	}
}

func mergeApprovalMetadata(parent, included map[string]any) error {
	parentDeclarations, err := approvalDeclarations(parent, "parent")
	if err != nil {
		return err
	}
	incomingDeclarations, err := approvalDeclarations(included, "included")
	if err != nil {
		return err
	}

	byName := make(map[string]compiler.Position, len(parentDeclarations)+len(incomingDeclarations))
	for _, declaration := range parentDeclarations {
		byName[declaration.Name] = declaration.Position
	}
	for _, declaration := range incomingDeclarations {
		if existing, duplicate := byName[declaration.Name]; duplicate {
			return compiler.SourceError(declaration.Position,
				"@approval %s: duplicate declaration (already at %s)", declaration.Name, existing)
		}
		byName[declaration.Name] = declaration.Position
	}

	// append to fresh storage so neither side's slice/backing array is modified
	// or exposed as the merged result.
	merged := make([]Declaration, 0, len(parentDeclarations)+len(incomingDeclarations))
	merged = append(merged, parentDeclarations...)
	merged = append(merged, incomingDeclarations...)
	parent[metadataKey] = merged
	return nil
}

func approvalDeclarations(meta map[string]any, side string) ([]Declaration, error) {
	value, exists := meta[metadataKey]
	if !exists {
		return nil, nil
	}
	declarations, ok := value.([]Declaration)
	if !ok {
		return nil, fmt.Errorf("%s metadata %q has type %T, want []approval.Declaration", side, metadataKey, value)
	}
	return declarations, nil
}
