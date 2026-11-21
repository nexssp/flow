// Package config_yaml registers a YAML decoder with extensions/config and the
// config_yaml.load runtime action. Users opt in with
//
//	@require config_yaml
//
// Only flows that declare it pull gopkg.in/yaml.v3 into their build.
package config_yaml

import (
	"embed"

	"github.com/nexssp/kernel/action"
	"gopkg.in/yaml.v3"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/config"
)

const ID = "config_yaml"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	config.RegisterYAMLLoader(unmarshal)
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{LoadAction}}},
		Fixtures:  fixturesFS,
	}
}

func unmarshal(data []byte) (map[string]any, error) {
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
