// Package on provides the `@on event "protocol:target"` directive,
// which records an EventTrigger into meta["on_event"]. The runtime
// reads it to decide whether `nflow serve` can start a listener for
// this .nflow file.
//
// Typical use:
//
//	@on event "nats:orders.incoming"
//	@on event "http::8080"
package on

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "on"

// EventTrigger is the structured result of an @on directive. Protocol
// and Target are split at the first ":" so "nats:orders.x" yields
// Protocol="nats" and Target="orders.x".
type EventTrigger struct {
	Protocol string
	Target   string
}

var directive = core.Directive{
	Name:    "on",
	Example: `@on event "http::8080"`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@on"))

	fields := strings.Fields(rest)
	if len(fields) >= 2 && fields[0] == "event" {
		target := strings.Trim(fields[1], `"'`)
		if proto, addr, ok := strings.Cut(target, ":"); ok {
			req.Out["on_event"] = EventTrigger{Protocol: proto, Target: addr}
		}
	}
	return core.DirectiveRes{Next: req.I + 1}, nil
}

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{directive},
	}
}
