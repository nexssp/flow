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
	// CompileValues carries explicitly typed modifier values without flattening
	// them through the legacy string callback.
	CompileValues func(name, source string, modifiers []ModifierValue) (action.AnyAction, error)
	// CompileWithAnalysisInput compiles a body with an initial static input type.
	// Materializers use it when a declaration provides a typed input contract.
	CompileWithAnalysisInput func(name, source string, modifiers []ModifierValue, input TypeDescriptor) (action.AnyAction, error)
	// CompileWithAnalysisBindings also seeds immutable lexical bindings for that body.
	CompileWithAnalysisBindings func(name, source string, modifiers []ModifierValue, input TypeDescriptor, bindings map[string]TypeDescriptor) (action.AnyAction, error)
	Alias                       string
	// Introspection enables retaining child-action relationships on materialized actions.
	Introspection bool
}

// Materializer converts declarations from meta into executable actions in the resolver.
type Materializer func(req MaterializeReq) error
