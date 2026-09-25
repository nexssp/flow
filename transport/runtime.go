package transport

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// Workload to uniwersalne wejście dla akcji triggera.
// Może to być:
//   - action.Library (zbiór akcji i streamów)
//   - action.AnyAction (pojedyncza akcja)
//   - action.AnyStreamAction (pojedynczy strumień!)
//   - []action.AnyAction
type Workload = any

// AsLibrary normalizuje dowolny workload (Action, StreamAction, Library, slice) do action.Library.
func AsLibrary(v any) action.Library {
	switch val := v.(type) {
	case action.Library:
		return val
	case action.AnyAction:
		return action.Library{Actions: []action.AnyAction{val}}
	case action.AnyStreamAction:
		return action.Library{Sources: []action.AnyStreamAction{val}}
	case []action.AnyAction:
		return action.Library{Actions: val}
	case []action.AnyStreamAction:
		return action.Library{Sources: val}
	default:
		return action.Library{}
	}
}

// RuntimeAction to sygnatura akcji nasłuchującej w transporcie.
type RuntimeAction func(context.Context, Workload) (any, error)
