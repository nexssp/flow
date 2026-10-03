// Package config_yaml registers a YAML decoder with extensions/config.
// It is not a native bundle: users opt in with
//
//	@require config_yaml
//
// Only flows that declare it pull gopkg.in/yaml.v3 into their build.
package config_yaml

import (
	"gopkg.in/yaml.v3"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/config"
)

const ID = "config_yaml"

func init() {
	core.Register(ID, Bundle)
	config.RegisterYAMLLoader(unmarshal)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		SelfTest:  selftest,
	}
}

func unmarshal(data []byte) (map[string]any, error) {
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
