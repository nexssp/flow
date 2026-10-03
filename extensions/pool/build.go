package pool

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

// buildPoolAction produces the runtime action for one declaration.
func buildPoolAction(declaration Declaration, resolver core.CapabilityResolver) (action.AnyAction, error) {
	members := make([]action.AnyAction, 0, len(declaration.Members))
	for _, ref := range declaration.Members {
		member, ok := resolver.Action(ref.Canonical)
		if !ok {
			return nil, xerr.NotFound(
				"member " + ref.Canonical + " not registered (declared as " + ref.Raw + ")")
		}
		members = append(members, member)
	}

	canonical := "pool." + declaration.Name

	switch declaration.Strategy {
	case "", "round_robin":
		return action.RoundRobinAny(canonical, members...).Build(), nil

	case "failover":
		return action.FirstSuccessAny(canonical, members...).Build(), nil

	case "hash":
		keyPath := declaration.Options["key"]
		if keyPath == "" {
			return nil, xerr.BadRequest(`strategy hash requires option key=".field.path"`)
		}
		extractor := makeStringPathExtractor(keyPath)
		return action.HashRouterAny(canonical, extractor, members...).Build(), nil

	default:
		return nil, xerr.BadRequest(
			"unknown strategy " + declaration.Strategy + " (known: round_robin, failover, hash)")
	}
}

// makeStringPathExtractor returns a key function that reads a dotted
// path from a nested map. Missing keys yield the empty string.
func makeStringPathExtractor(path string) func(any) string {
	clean := strings.TrimPrefix(path, ".")
	return func(input any) string {
		current := input
		for segment := range strings.SplitSeq(clean, ".") {
			m, ok := current.(map[string]any)
			if !ok {
				return ""
			}
			current, ok = m[segment]
			if !ok {
				return ""
			}
		}
		return fmt.Sprint(current)
	}
}
