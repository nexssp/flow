package compiler

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// MaterializePhase orders extension materializers within the compile lifecycle.
// Materializers in the same phase retain bundle registration order.
type MaterializePhase int

const (
	// MaterializeDeclarations mounts actions declared in extension metadata.
	// The zero value preserves the behavior of existing extension bundles.
	MaterializeDeclarations MaterializePhase = iota
	// MaterializeCompositions compiles declarations that consume mounted actions.
	MaterializeCompositions
)

// MaterializeReq passes context, metadata, and compiler to the materialization phase.
type MaterializeReq struct {
	Ctx      context.Context
	Meta     map[string]any
	Resolver CapabilityResolver
	Compile  func(name, source string, modifiers ...string) (action.AnyAction, error)
	Alias    string
	// Introspection enables retaining child-action relationships on materialized actions.
	Introspection bool
}

// Materializer converts declarations from meta into executable actions in the resolver.
type Materializer func(req MaterializeReq) error
