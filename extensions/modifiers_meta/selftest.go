package modifiers_meta

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Modifiers",
			Features: []core.SelfTestFeature{
				stringFeature("name", "coverage_name"),
				stringFeature("desc", `"a description"`),
				stringFeature("scope", "public"),
				stringFeature("status", "201"),
				stringFeature("tag", `"test,coverage"`),
				flagFeature("read_only"),
				flagFeature("debug"),
				flagFeature("deprecated"),
				flagFeature("audit"),
			},
		},
	}
}

func stringFeature(name, value string) core.SelfTestFeature {
	return core.SelfTestFeature{
		Name: "Modifier :" + name + "=",
		DSL: "runtime.const:" + name + "=" + value + ` @{ value: "ok" }` + "\n" +
			`@assert: result == "ok"`,
	}
}

func flagFeature(name string) core.SelfTestFeature {
	return core.SelfTestFeature{
		Name: "Modifier :" + name,
		DSL: "runtime.const:" + name + ` @{ value: "ok" }` + "\n" +
			`@assert: result == "ok"`,
	}
}
