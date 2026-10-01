package loop

import "github.com/nexssp/flow/core"

// selftest returns the loop keyword's DSL feature checks.
//
// The state field is named `n`, not `count`: expr-lang exposes `count`
// as a builtin, so `count` in an until clause would shadow the field
// with the builtin and never terminate.
func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Operators",
			Features: []core.SelfTestFeature{
				{
					Name: "Loop loop(...) until(...)",
					DSL: `@assert: result.n == 3
{ n: 0 } -> loop( { n: .n + 1 } ) until( .n >= 3 )`,
				},
			},
		},
	}
}
