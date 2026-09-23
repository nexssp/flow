package flow

import (
	"reflect"
	"slices"

	"github.com/nexssp/kernel/action"
)

// resolveCapability finds the action a node refers to.
//
// The lookup is two-phase:
//
//  1. Exact match against the registry. This is the common case: the
//     capability is a registered action, and the compiler returns it
//     as-is.
//  2. Fallback to CompilePipeline. This handles the case where a node
//     is itself a small inline pipeline expressed as an atom, for
//     example "log.info -> agent.critic" in a YAML graph.
//
// The fallback is attempted only when the exact lookup fails. That way
// a capability name that happens to also parse as a pipeline is never
// silently turned into one; it must be a registered action.
func (c *Compiler) resolveCapability(capName string) (action.AnyAction, bool) {
	if c.registry == nil {
		return nil, false
	}

	if act, ok := c.registry.Get(capName); ok {
		return act, true
	}

	if bld, err := CompilePipeline(capName, c.registry); err == nil && bld != nil {
		return bld.Build(), true
	}

	return nil, false
}

// unpackIntoMap copies fields of src into dst. It understands two
// shapes:
//
//	map[string]any       — copied key by key
//	struct or *struct    — each exported field is copied under both its
//	                       Go field name and its json tag name
//
// The function is deliberately tolerant: unknown shapes are ignored
// rather than rejected, because callers use it to flatten whatever an
// upstream node happened to produce. A node whose output cannot be
// unpacked is still available under its own node ID in the payload,
// so no information is lost.
func unpackIntoMap(dst map[string]any, src any) {
	if src == nil {
		return
	}

	if m, ok := src.(map[string]any); ok {
		for k, v := range m {
			dst[k] = v
		}
		return
	}

	rv := reflect.ValueOf(src)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return
	}

	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		val := rv.Field(i).Interface()
		dst[f.Name] = val

		tag := f.Tag.Get("json")
		if tag != "" && tag != "-" {
			name := tag
			if comma := slices.Index([]byte(tag), ','); comma >= 0 {
				name = tag[:comma]
			}
			if name != "" {
				dst[name] = val
			}
		}
	}
}
