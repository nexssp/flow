package core

import (
	"reflect"
	"strings"

	"github.com/nexssp/kernel/action"
)

// SuggestModifierFix returns a human-readable explanation for why a
// modifier name cannot be applied to a given action, or empty when no
// better fix than "unknown modifier" is available.
//
// The dominant real-world mistake: the author writes :file=... where
// the action takes @{ file: ... }. The check against the action's Req
// struct catches that before the runtime sees a modifier-table miss.
func SuggestModifierFix(target action.AnyAction, name string) string {
	fields := requestFieldNames(target)
	if _, ok := fields[name]; !ok {
		return ""
	}
	return "modifier :" + name + " is not registered, but " +
		actionDisplayName(target) + " takes a request field \"" + name + "\"; " +
		"use @{ " + name + ": ... } instead"
}

// requestFieldNames returns the JSON names of every exported field of
// the target's request payload, including fields promoted from
// anonymous embedded structs. Returns nil when the target has no typed
// payload or the payload is not a struct — a wrapped action such as
// the injection builder loses the concrete Req type, and there is
// nothing to inspect.
func requestFieldNames(target action.AnyAction) map[string]bool {
	if target == nil {
		return nil
	}
	typed, ok := target.(action.TypedPayload)
	if !ok {
		return nil
	}
	payload := typed.ReqPayload()
	if payload == nil {
		return nil
	}

	t := reflect.TypeOf(payload)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	out := make(map[string]bool, t.NumField())
	collectRequestFieldNames(t, out)
	return out
}

func collectRequestFieldNames(t reflect.Type, out map[string]bool) {
	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("json")
		tagName, _, _ := strings.Cut(tag, ",")

		// Anonymous embedded struct with no JSON name: its fields are
		// promoted into the parent object, so recurse instead of
		// listing the embedding itself.
		if field.Anonymous && tagName == "" && field.Type.Kind() == reflect.Struct {
			collectRequestFieldNames(field.Type, out)
			continue
		}
		if tagName == "-" {
			continue
		}
		if tagName == "" {
			tagName = field.Name
		}
		out[tagName] = true
	}
}

func actionDisplayName(target action.AnyAction) string {
	if meta := target.Describe(); meta != nil && meta.Name != "" {
		return meta.Name
	}
	return "this action"
}
