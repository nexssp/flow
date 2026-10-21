package pool

import (
	"fmt"

	compiler "github.com/nexssp/flow/compiler"
)

func metadataMerger() *compiler.MetadataMerger {
	const key = "pools"
	return &compiler.MetadataMerger{
		Keys: []string{key},
		Merge: func(parent, included map[string]any) error {
			parentDecls, err := declarationsValue(parent, key, "parent")
			if err != nil {
				return err
			}
			includedDecls, err := declarationsValue(included, key, "included")
			if err != nil {
				return err
			}
			merged := make([]Declaration, len(parentDecls), len(parentDecls)+len(includedDecls))
			copy(merged, parentDecls)
			seen := make(map[string]struct{}, len(merged)+len(includedDecls))
			for _, decl := range merged {
				seen[decl.Name] = struct{}{}
			}
			for _, decl := range includedDecls {
				if _, exists := seen[decl.Name]; exists {
					return duplicateDeclaration(decl)
				}
				seen[decl.Name] = struct{}{}
				merged = append(merged, decl)
			}
			parent[key] = merged
			return nil
		},
	}
}

func declarationsValue(meta map[string]any, key, side string) ([]Declaration, error) {
	value, exists := meta[key]
	if !exists {
		return nil, nil
	}
	decls, ok := value.([]Declaration)
	if !ok {
		return nil, fmt.Errorf("metadata %q: expected []Declaration in %s metadata, got %T", key, side, value)
	}
	return decls, nil
}

func duplicateDeclaration(decl Declaration) error {
	if decl.Position.File != "" {
		return compiler.SourceError(decl.Position, "@pool %s: duplicate declaration", decl.Name)
	}
	return fmt.Errorf("@pool %s: duplicate declaration", decl.Name)
}
