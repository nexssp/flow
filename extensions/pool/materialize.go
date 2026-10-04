package pool

import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

// materialize mounts one `pool.<name>` action per declaration. The
// action's shape depends on the declared strategy.
func materialize(req core.MaterializeReq) error {
	for _, declaration := range DeclarationsFromMeta(req.Meta) {
		act, err := buildPoolAction(declaration, req.Resolver)
		if err != nil {
			return xerr.BadRequest("@pool "+declaration.Name+": "+err.Error(), err)
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
