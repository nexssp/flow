package config

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Config",
			Features: []core.SelfTestFeature{
				{
					Name: "@config: single line",
					DSL: `@config:strict=true
@config:silent=coverage
@assert: result == "c"
runtime.const @{ value: "c" }`,
				},
				{
					Name: "@config block with inline modifiers",
					DSL: `@config {
  retries: 3       :cli="r,retries" :desc="Max attempts"
  timeout: "10s"   :cli="t"
}
@assert: result.retries == "3"
@assert: result.timeout == "10s"
runtime.const @{ value: { retries: "@config.retries", timeout: "@config.timeout" } }`,
				},
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
