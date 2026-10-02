package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
)

var DispatchByPrefix = action.New("runtime.dispatch_by_prefix", func(ctx context.Context, in any) (any, error) {
	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("dispatch_by_prefix: input must be an object")
	}

	prefix := strings.TrimSpace(readStringArg(m, "prefix"))
	key := strings.TrimSpace(readStringArg(m, "key"))
	if prefix == "" || key == "" {
		return nil, xerr.BadRequest("dispatch_by_prefix: prefix and key are required")
	}

	name := prefix + "." + key
	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return nil, xerr.Internal("dispatch_by_prefix: no action resolver in execution context")
	}

	target, ok := resolver.Action(name)
	if !ok {
		return nil, xerr.NotFound(fmt.Sprintf("dispatch_by_prefix: action %q is not in the registry", name))
	}

	payload := m["payload"]
	if payload == nil {
		payload = in
	}
	return action.InvokeAny(ctx, target, payload)
}).Description("Resolve `prefix.key` against the registry and invoke it").
	Tag("base", "dynamic").
	Build()
