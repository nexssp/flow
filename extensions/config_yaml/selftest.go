package config_yaml

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Config YAML",
			Features: []core.SelfTestFeature{
				{
					Name: "@config.load YAML",
					DSL: `@config.load:path="nexss.yml"
@assert: result.host == "api.example.test"
runtime.const @{ value: { host: "@config.host" } }`,
					Files: map[string]string{
						"nexss.yml": "host: api.example.test\nport: 8443\n",
					},
				},
			},
		},
	}
}
