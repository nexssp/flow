package scope

import (
	"fmt"
	"maps"

	compiler "github.com/nexssp/flow/compiler"
)

func profilesMetadataMerger() *compiler.MetadataMerger {
	return &compiler.MetadataMerger{
		Keys: []string{profilesKey},
		Merge: func(parent, included map[string]any) error {
			parentProfiles, err := profileMap(parent, "parent")
			if err != nil {
				return err
			}
			includedProfiles, err := profileMap(included, "included")
			if err != nil {
				return err
			}
			merged := make(map[string]profileDef, len(parentProfiles)+len(includedProfiles))
			maps.Copy(merged, parentProfiles)
			for name, profile := range includedProfiles {
				if _, exists := merged[name]; exists {
					return fmt.Errorf("@profile %s: duplicate declaration", name)
				}
				merged[name] = profile
			}
			parent[profilesKey] = merged
			return nil
		},
	}
}

func profileMap(meta map[string]any, side string) (map[string]profileDef, error) {
	value, exists := meta[profilesKey]
	if !exists {
		return map[string]profileDef{}, nil
	}
	profiles, ok := value.(map[string]profileDef)
	if !ok {
		return nil, fmt.Errorf("metadata %q: expected map[string]profileDef in %s metadata, got %T", profilesKey, side, value)
	}
	return profiles, nil
}
