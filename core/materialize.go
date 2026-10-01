package core

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// MaterializeReq przekazuje kontekst, metadane i kompilator do fazy materializacji.
type MaterializeReq struct {
	Ctx      context.Context
	Meta     map[string]any
	Resolver CapabilityResolver
	Compile  func(name, source string) (action.AnyAction, error)
	Alias    string
}

// Materializer zamienia deklaracje z meta na działające akcje w resolverze.
type Materializer func(req MaterializeReq) error
