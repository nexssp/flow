package schema

import (
	"fmt"

	compiler "github.com/nexssp/flow/compiler"
)

func metadataMerger() *compiler.MetadataMerger {
	const key = "schemas"
	return &compiler.MetadataMerger{
		Keys: []string{key},
		Merge: func(parent, included map[string]any) error {
			parentSchemas, err := schemasValue(parent, key, "parent")
			if err != nil {
				return err
			}
			includedSchemas, err := schemasValue(included, key, "included")
			if err != nil {
				return err
			}
			merged := make([]Schema, len(parentSchemas), len(parentSchemas)+len(includedSchemas))
			copy(merged, parentSchemas)
			seen := make(map[string]struct{}, len(merged)+len(includedSchemas))
			for _, schema := range merged {
				seen[schema.Name] = struct{}{}
			}
			for _, schema := range includedSchemas {
				if _, exists := seen[schema.Name]; exists {
					return fmt.Errorf("@schema %s: duplicate declaration", schema.Name)
				}
				seen[schema.Name] = struct{}{}
				merged = append(merged, schema)
			}
			parent[key] = merged
			return nil
		},
	}
}

func schemasValue(meta map[string]any, key, side string) ([]Schema, error) {
	value, exists := meta[key]
	if !exists {
		return nil, nil
	}
	schemas, ok := value.([]Schema)
	if !ok {
		return nil, fmt.Errorf("metadata %q: expected []Schema in %s metadata, got %T", key, side, value)
	}
	return schemas, nil
}
