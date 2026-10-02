package runtime

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
)

var Call = action.New("runtime.call", func(ctx context.Context, in any) (any, error) {
	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("call: input must be an object carrying 'name' and optional 'payload'")
	}

	name := strings.TrimSpace(readStringArg(m, "name"))
	if name == "" {
		return nil, xerr.BadRequest(`call: 'name' is required (use: { name: "log.info", payload: { ... } } -> runtime.call)`)
	}

	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return nil, xerr.Internal("call: no action resolver in execution context")
	}

	target, ok := resolver.Action(name)
	if !ok {
		return nil, xerr.NotFound("call: target action " + name + " not found in registry")
	}

	payload, hasPayload := m["payload"]
	if !hasPayload || payload == nil {
		filtered := make(map[string]any, len(m))
		for k, v := range m {
			if k != "name" {
				filtered[k] = v
			}
		}
		payload = filtered
	}
	return action.InvokeAny(ctx, target, payload)
}).Description("Resolve and invoke another action by name at runtime").
	Tag("base", "dynamic").
	Build()
