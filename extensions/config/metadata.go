package config

import (
	"fmt"
	"maps"

	"github.com/nexssp/flow/compiler"
)

func configMetadata() *compiler.MetadataMerger {
	return &compiler.MetadataMerger{Keys: []string{"config"}, Merge: mergeConfigMetadata}
}

func mergeConfigMetadata(parent, included map[string]any) error {
	local, err := configMetadataValue(parent)
	if err != nil {
		return err
	}
	defaults, err := configMetadataValue(included)
	if err != nil {
		return err
	}
	merged := make(map[string]string, len(local)+len(defaults))
	maps.Copy(merged, defaults)
	maps.Copy(merged, local)
	parent["config"] = merged
	return nil
}

func configMetadataValue(metadata map[string]any) (map[string]string, error) {
	value, exists := metadata["config"]
	if !exists {
		return map[string]string{}, nil
	}
	settings, ok := value.(map[string]string)
	if !ok {
		return nil, fmt.Errorf("config metadata must be map[string]string, got %T", value)
	}
	return settings, nil
}
