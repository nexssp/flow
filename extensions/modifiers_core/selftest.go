package modifiers_core

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Modifiers",
			Features: []core.SelfTestFeature{
				modifierFeature("timeout", "1s"),
				modifierFeature("retry", "3"),
				modifierFeature("concurrency", "4"),
				modifierFeature("rate_limit", "100"),
				modifierFeature("cache", "1m"),
				modifierFlag("coalesce"),
				modifierFlag("dedup"),
				modifierFlag("idempotent"),
			},
		},
	}
}

func modifierFeature(name, value string) core.SelfTestFeature {
	return core.SelfTestFeature{
		Name: "Modifier :" + name + "=",
		DSL: "runtime.const:" + name + "=" + value + ` @{ value: "ok" }` + "\n" +
			`@assert: result == "ok"`,
	}
}

func modifierFlag(name string) core.SelfTestFeature {
	return core.SelfTestFeature{
		Name: "Modifier :" + name,
		DSL: "runtime.const:" + name + ` @{ value: "ok" }` + "\n" +
			`@assert: result == "ok"`,
	}
}
