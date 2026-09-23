package dslparse

// The `:hook=name` modifier attaches a named hook to a single atom.
//
// Multiple hook names can be supplied, separated by commas:
//
//	agent.critic:hook=audit,telemetry
//
// Names are resolved at compile time via action.NamedHook. An unknown
// name produces a compile error rather than a silent no-op.
func init() {
	registerMod("hook", func(p *Modifiers, v string, _ bool) error {
		for _, name := range splitCSV(v) {
			if name != "" {
				p.HookNames = append(p.HookNames, name)
			}
		}
		return nil
	})
}
