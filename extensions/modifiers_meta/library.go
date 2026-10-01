// Package modifiers_meta ships the metadata modifiers :name=, :desc=,
// :description=, :status=, :tag=, :scope=, and the flags :read_only,
// :audit, :debug, :deprecated, :strict, :lenient. All are visible in
// the catalog and CLI; none changes the runtime hot path.
//
// Typical use:
//
//	order.create:name="order.create":tag="orders,mutation":scope=public
//
// :strict and :lenient are recognized by core/strict.go through
// hasModifier and toggle runtime request validation; their apply
// callback is deliberately an identity function.
package modifiers_meta

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "modifiers_meta"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Modifiers: Modifiers(),
		SelfTest:  selftest,
	}
}
