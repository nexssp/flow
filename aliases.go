// path: nexssp/flow/aliases.go
//
// Human-readable aliases for registered actions. An alias points at the
// SAME *BuiltAction as its canonical name; no wrapper, no reflection.
// Resolved once at boot; the DSL compiler sees an ordinary registry entry.
//
// A canonical name always wins over an alias for the same word, so an
// application can safely define its own `read` action.
package flow

// DefaultAliases maps a canonical action name to the short words a
// non-programmer can type in a .flow file.
//
// Append from an init() function to add application-specific words.
// Not safe for concurrent mutation at runtime.
var DefaultAliases = map[string][]string{
	// workspace
	"workspace.read_file":  {"read", "open", "read_file"},
	"workspace.write_file": {"write", "save_file", "write_file"},
	"workspace.edit_file":  {"edit", "edit_file"},

	// prompts
	"prompt.summarize":   {"summarize", "summary"},
	"prompt.translate":   {"translate"},
	"prompt.code_review": {"review", "review_code"},

	// llm
	"ai.complete": {"llm"},
	"ai.prompt":   {"ask"},

	// agents
	"agent.planner":   {"plan", "planner"},
	"agent.architect": {"code", "write_code", "fix"},
	"agent.critic":    {"critic", "evaluate"},
	"agent.swarm":     {"swarm"},

	// sandbox
	"sandbox.exec":        {"run", "exec", "shell"},
	"sandbox.test":        {"test", "run_tests"},
	"sandbox.test_runner": {"test_runner"},

	// bench
	"bench.run":     {"bench", "benchmark"},
	"bench.save":    {"save_bench"},
	"bench.compare": {"compare", "diff"},

	// log
	"log.info":  {"log", "info"},
	"log.warn":  {"warn"},
	"log.error": {"error"},

	// distribute
	"distribute.map":    {"map", "fanout", "parallel"},
	"distribute.reduce": {"reduce", "fold"},
}

// RegisterAliases walks reg once and adds every alias that does not
// collide with an existing canonical name. Safe to call more than once.
func RegisterAliases(reg *MapRegistry) {
	if reg == nil {
		return
	}

	for _, act := range reg.Actions() {
		meta := act.Describe()
		if meta == nil {
			continue
		}

		for _, alias := range DefaultAliases[meta.Name] {
			if _, exists := reg.Get(alias); exists {
				continue
			}

			reg.Register(alias, act)
		}
	}
}
