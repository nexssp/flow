package core

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// MaterializeReq passes context, metadata, and compiler to the materialization phase.
type MaterializeReq struct {
	Ctx      context.Context
	Meta     map[string]any
	Resolver CapabilityResolver
	Compile  func(name, source string, modifiers ...string) (action.AnyAction, error)
	Alias    string
}

// Materializer converts declarations from meta into executable actions in the resolver.
type Materializer func(req MaterializeReq) error
