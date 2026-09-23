package utility

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// NewCallAction returns an action that resolves another action by name
// from the registry installed on the execution context, then invokes it.
//
// Input: { name: "grade.a", payload: <optional> }. When payload is
// omitted, the whole input object is forwarded.
//
// Security: call reaches anything in the registry. It is not a sandbox.
// If a flow processes untrusted input and the registry contains
// sensitive actions, restrict the flow's @profile or wrap call with an
// allowlist hook.
func NewCallAction() action.AnyAction {
	return action.New("call", func(ctx context.Context, in any) (any, error) {
		m, ok := in.(map[string]any)
		if !ok {
			return nil, xerr.BadRequest(fmt.Sprintf(
				"call: input must be an object carrying 'name' and optional 'payload', got %T\n"+
					"              ├── hint      : pass { name: \"action_name\", payload: { ... } }",
				in,
			))
		}

		name := strings.TrimSpace(readStringArg(m, "name"))
		if name == "" {
			return nil, xerr.BadRequest(
				"call: 'name' is required in input\n" +
					"              ├── hint      : example: { name: \"log.info\", payload: { message: \"hello\" } } -> call",
			)
		}

		reg := contracts.RegistryFromContext(ctx)
		if reg == nil {
			return nil, xerr.Internal("call: no action registry available in execution context")
		}

		target, ok := reg.Get(name)
		if !ok {
			var available []string
			for _, a := range reg.Actions() {
				if a != nil && a.Describe() != nil {
					available = append(available, a.Describe().Name)
				}
			}
			return nil, xerr.NotFound(fmt.Sprintf(
				"call: target action %q not found in registry\n"+
					"              ├── available : %v\n"+
					"              ├── hint      : verify spelling or ensure the library is @require'd",
				name, available,
			))
		}

		payload, hasPayload := m["payload"]
		if !hasPayload || payload == nil {
			// Przekaż cały obiekt bez klucza "name", jeśli brak dedykowanego "payload"
			filtered := make(map[string]any, len(m))
			for k, v := range m {
				if k != "name" {
					filtered[k] = v
				}
			}
			payload = filtered
		}

		return action.InvokeAny(ctx, target, payload)
	}).
		Description("Resolve and invoke another action by name at runtime").
		Tag("base", "dynamic").
		Build()
}

// NewDispatchByPrefixAction is the ergonomic form of call when the
// target name is `prefix + "." + key`. Same semantics, one less string
// concatenation in the .nflow file.
//
// Input: { prefix: "grade", key: "a", payload: <optional> }.
func NewDispatchByPrefixAction() action.AnyAction {
	return action.New("dispatch_by_prefix", func(ctx context.Context, in any) (any, error) {
		m, ok := in.(map[string]any)
		if !ok {
			return nil, xerr.BadRequest("dispatch_by_prefix: input must be an object")
		}

		prefix := strings.TrimSpace(readStringArg(m, "prefix"))
		key := strings.TrimSpace(readStringArg(m, "key"))
		if prefix == "" || key == "" {
			return nil, xerr.BadRequest(
				"dispatch_by_prefix: prefix and key are required")
		}

		name := prefix + "." + key

		reg := contracts.RegistryFromContext(ctx)
		if reg == nil {
			return nil, xerr.Internal(
				"dispatch_by_prefix: no registry in execution context")
		}
		target, ok := reg.Get(name)
		if !ok {
			return nil, xerr.NotFound(fmt.Sprintf(
				"dispatch_by_prefix: action %q is not in the registry", name))
		}

		payload := m["payload"]
		if payload == nil {
			payload = in
		}
		return action.InvokeAny(ctx, target, payload)
	}).
		Description("Resolve `prefix.key` against the registry and invoke it").
		Tag("base", "dynamic").
		Build()
}
