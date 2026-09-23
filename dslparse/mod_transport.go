package dslparse

// Transport modifiers. Most delegate to buildDSLTransportBinding in
// scan.go; route, http, and a few extras are handled inline.
//
//	:route=, :http=, :channel=
//	:cli_alias=, :cli_desc=, :a2a_desc=, :a2a_example=
//
// All other transport keys (sse, raw, cli, cron, worker, topic, nats*,
// a2a) fall through applyModifiers to the transport binding builder.
func init() {
	route := func(p *Modifiers, v string, _ bool) error {
		method, path := parseRouteValue(v, "POST")
		p.Method = method
		p.Path = path
		if path != "" {
			p.Transports = append(p.Transports, TransportBinding{
				Kind: "http", Protocol: "thttp",
				Target: method + " " + path,
				Method: method, Path: path,
			})
		}
		return nil
	}
	registerMod("route", route)
	registerMod("http", route)

	registerMod("channel", func(p *Modifiers, v string, _ bool) error {
		p.SSEChannel = trimValue(v)
		return nil
	})

	registerMod("cli_alias", func(p *Modifiers, v string, _ bool) error {
		p.CLIAliases = append(p.CLIAliases, splitCSV(v)...)
		return nil
	})

	registerMod("cli_desc", func(p *Modifiers, v string, _ bool) error {
		p.CLIDesc = trimValue(v)
		return nil
	})

	registerMod("a2a_desc", func(p *Modifiers, v string, _ bool) error {
		p.A2ADesc = trimValue(v)
		return nil
	})

	registerMod("a2a_example", func(p *Modifiers, v string, _ bool) error {
		p.A2AExample = append(p.A2AExample, splitCSV(v)...)
		return nil
	})
}
