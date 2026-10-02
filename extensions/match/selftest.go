package match

import "github.com/nexssp/flow/core"

func selftest() []core.SelfTestSection {
	return []core.SelfTestSection{
		{
			Name: "Match",
			Features: []core.SelfTestFeature{
				{
					Name: "match subject equality",
					DSL: `@assert: result.priority == "URGENT"
{ code: "ACTION" }
-> match(.code) {
  "ACTION" -> { priority: "URGENT" },
  "WATCH"  -> { priority: "MONITOR" },
  _        -> { priority: "ROUTINE" }
}`,
				},
				{
					Name: "match boolean predicates",
					DSL: `@assert: result.status == "HIGH_RISK"
{ danger: true, blocked: false }
-> match {
  .danger              -> { status: "HIGH_RISK" },
  .blocked && !.danger -> { status: "HOLD" },
  _                    -> { status: "NORMAL" }
}`,
				},
				{
					Name: "match default fallback",
					DSL: `@assert: result.status == "NORMAL"
{ danger: false, blocked: false }
-> match {
  .danger -> { status: "HIGH_RISK" },
  _       -> { status: "NORMAL" }
}`,
				},
			},
		},
	}
}
