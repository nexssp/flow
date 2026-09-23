package runner

import (
	"fmt"

	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/action"
)

// DeclMaterializer converts one domain's declarations into actions.
//
// A domain package (ai/llm, ai/sandbox) registers a materializer via
// init(). When a .nflow file declares something in that domain — for
// example @llm router { provider: "deepseek", model: "..." } — the
// materializer is called to turn the declaration into a runnable
// action. The name in the .nflow file becomes the action's registry
// name.
//
// The interface lives in flow/runner, not in flow/directives, because
// it depends on action.AnyAction. flow/directives is parse-only; the
// runner is the assembly point.
type DeclMaterializer interface {
	// Key is the same key the domain stores its declarations under in
	// Preprocessed.Declarations. "llm", "sandbox", ...
	Key() string

	// Materialize turns the raw declaration value into actions. The
	// value's concrete type is the domain's business: the runner only
	// guarantees that it is whatever Preprocessed.Declarations[key]
	// contained.
	Materialize(decls any, base *action.Registry) ([]action.AnyAction, error)
}

var materializers = map[string]DeclMaterializer{}

// RegisterMaterializer is called from init() of a domain package.
// Panics on duplicate keys — a load-time failure beats a runtime
// silent override.
func RegisterMaterializer(m DeclMaterializer) {
	if m == nil {
		panic("runner: RegisterMaterializer(nil)")
	}
	key := m.Key()
	if key == "" {
		panic("runner: materializer with empty Key()")
	}
	if _, dup := materializers[key]; dup {
		panic("runner: duplicate materializer for key " + key)
	}
	materializers[key] = m
}

// materializeFromPreprocessed walks every declaration in pre and asks
// the registered materializer to produce actions. When no materializer
// matches a key, the declaration is ignored — that is how a domain
// package that was not imported still allows the flow to load, as long
// as nothing tries to use its declarations.
//
// The returned registry always contains the base actions; the derived
// ones are added on top. On error the base registry is left untouched
// and the caller sees the domain-specific message.
func materializeFromPreprocessed(pre *directives.Preprocessed, base *action.Registry) (*action.Registry, error) {
	if pre == nil || len(pre.Declarations) == 0 {
		return base, nil
	}

	var derived []action.AnyAction

	for key, value := range pre.Declarations {
		m, ok := materializers[key]
		if !ok {
			continue
		}
		acts, err := m.Materialize(value, base)
		if err != nil {
			return nil, fmt.Errorf("materialize @%s: %w", key, err)
		}
		derived = append(derived, acts...)
	}

	if len(derived) == 0 {
		return base, nil
	}

	libs := make([]action.Library, 0, 2)
	if base != nil && len(base.Actions()) > 0 {
		libs = append(libs, action.Library{Name: "base", Actions: base.Actions()})
	}
	libs = append(libs, action.Library{Name: "derived", Actions: derived})

	return action.NewRegistry(libs...)
}
