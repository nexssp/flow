package pool

import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	core "github.com/nexssp/flow/compiler"
)

// materialize mounts one `pool.<name>` action per declaration. The
// action's shape depends on the declared strategy.
func materialize(req core.MaterializeReq) error {
	for _, declaration := range DeclarationsFromMeta(req.Meta) {
		act, err := buildPoolAction(declaration, req.Resolver)
		if err != nil {
			return xerr.BadRequest("@pool "+declaration.Name+": "+err.Error(), err)
		}
		if req.Introspection {
			act = action.WithComposition(act, poolComposition(declaration, req.Resolver))
		}
		if err := req.Resolver.Mount(action.Library{
			Name:    "pool." + declaration.Name,
			Actions: []action.AnyAction{act},
		}); err != nil {
			return xerr.Internal("@pool "+declaration.Name+": mount: "+err.Error(), err)
		}
	}
	return nil
}

func poolComposition(declaration Declaration, resolver core.CapabilityResolver) action.IntrospectionComposition {
	kind, relation, role := "pool.round_robin", "route", "member"
	notes := []string(nil)
	switch declaration.Strategy {
	case "failover":
		kind, relation, role = "pool.failover", "fallback", "candidate"
	case "hash":
		kind = "pool.hash"
		notes = []string{"hash selector and configuration values are omitted"}
	}
	children := make([]action.IntrospectionChild, 0, len(declaration.Members))
	for _, member := range declaration.Members {
		act, ok := resolver.Action(member.Canonical)
		if !ok {
			continue
		}
		children = append(children, action.IntrospectionChild{
			Node: act, Relation: relation, Role: role, Label: member.Canonical,
		})
	}
	return action.IntrospectionComposition{
		Kind: kind, Opaque: len(notes) > 0, Notes: notes, Children: children,
	}
}
