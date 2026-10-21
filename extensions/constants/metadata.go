package constants

import (
	"maps"

	"github.com/nexssp/flow/compiler"
)

func constantsMetadata() *compiler.MetadataMerger {
	return &compiler.MetadataMerger{
		Keys:       []string{"constants"},
		Merge:      mergeConstantsMetadata,
		AfterMerge: applyMergedConstants,
	}
}

func mergeConstantsMetadata(parent, included map[string]any) error {
	incoming, _ := included["constants"].(map[string]string)
	parentConstants, _ := parent["constants"].(map[string]string)
	merged := make(map[string]string, len(parentConstants)+len(incoming))
	maps.Copy(merged, parentConstants)
	// The including file is more local, so values already declared there win.
	for key, value := range incoming {
		if _, exists := merged[key]; !exists {
			merged[key] = value
		}
	}
	parent["constants"] = merged
	return nil
}

func applyMergedConstants(req compiler.DirectiveReq) error {
	if consts, ok := req.Out["constants"].(map[string]string); ok {
		compiler.ApplyConstants(req.Lines, req.I+1, consts)
	}
	return nil
}
