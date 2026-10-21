package require

import (
	"fmt"

	compiler "github.com/nexssp/flow/compiler"
)

func metadataMerger() *compiler.MetadataMerger {
	const key = "require"
	return &compiler.MetadataMerger{
		Keys: []string{key},
		Merge: func(parent, included map[string]any) error {
			parentReqs, err := requirementsValue(parent, key, "parent")
			if err != nil {
				return err
			}
			includedReqs, err := requirementsValue(included, key, "included")
			if err != nil {
				return err
			}

			merged := make([]Requirement, len(parentReqs), len(parentReqs)+len(includedReqs))
			copy(merged, parentReqs)
			for i := range includedReqs {
				candidate := &includedReqs[i]
				duplicate := false
				for j := range merged {
					prior := &merged[j]
					if prior.Import == candidate.Import && prior.Version == candidate.Version && prior.Alias == candidate.Alias {
						// This is the same declaration according to @require's
						// existing handler policy. Preserve the parent's options.
						duplicate = true
						break
					}
					if candidate.Alias != "" && prior.Alias == candidate.Alias {
						return fmt.Errorf("@require: namespace qualifier %q is already used for module %q", candidate.Alias, prior.Import)
					}
				}
				if !duplicate {
					merged = append(merged, *candidate)
				}
			}
			parent[key] = merged
			return nil
		},
	}
}

func requirementsValue(meta map[string]any, key, side string) ([]Requirement, error) {
	value, exists := meta[key]
	if !exists {
		return nil, nil
	}
	reqs, ok := value.([]Requirement)
	if !ok {
		return nil, fmt.Errorf("metadata %q: expected []Requirement in %s metadata, got %T", key, side, value)
	}
	return reqs, nil
}
