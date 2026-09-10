// flow/cost.go
package flow

import (
	"github.com/nexssp/cost"
	kernelcost "github.com/nexssp/cost/adapters/kernel"
	"github.com/nexssp/kernel/action"
)

// GuardCost returns an action hook that reserves budget before execution
// and commits or releases it upon completion based on execution outcome.
func GuardCost(reserver cost.Reserver, estimateMicros int64) action.AnyHook {
	return kernelcost.GuardAction(reserver, estimateMicros)
}

// AsCostHook provides an alias for GuardCost to attach cost governance hooks to actions.
func AsCostHook(reserver cost.Reserver, estimateMicros int64, _ ...int64) action.AnyHook {
	return kernelcost.GuardAction(reserver, estimateMicros)
}
