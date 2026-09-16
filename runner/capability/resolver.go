package capability

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
)

type Resolver struct {
	registry *action.Registry
	bindings []Binding
	built    map[string]action.AnyAction
	order    []string
	cleanups []func() error
}

func NewResolver(ctx context.Context, registry *action.Registry, manifest string) (*Resolver, error) {
	bindings := ParseBindings(manifest)

	r := &Resolver{
		registry: registry,
		bindings: bindings,
		built:    make(map[string]action.AnyAction, len(bindings)),
		order:    make([]string, 0, len(bindings)),
	}

	for _, b := range bindings {
		if registry != nil {
			if _, ok := registry.Get(b.Name); ok {
				continue
			}
		}

		act, cleanup, err := r.build(ctx, b)
		if err != nil {
			_ = r.Close()
			return nil, err
		}
		if _, dup := r.built[b.Name]; !dup {
			r.order = append(r.order, b.Name)
		}
		r.built[b.Name] = act
		if cleanup != nil {
			r.cleanups = append(r.cleanups, cleanup)
		}
	}

	return r, nil
}

func (r *Resolver) Resolve(name string) (action.AnyAction, bool) {
	if r == nil {
		return nil, false
	}
	if act, ok := r.built[name]; ok {
		return act, true
	}
	if r.registry != nil {
		return r.registry.Get(name)
	}
	return nil, false
}

func (r *Resolver) Actions() []action.AnyAction {
	if r == nil {
		return nil
	}
	out := make([]action.AnyAction, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.built[name])
	}
	return out
}

func (r *Resolver) Bindings() []Binding {
	if r == nil {
		return nil
	}
	return r.bindings
}

func (r *Resolver) Close() error {
	if r == nil {
		return nil
	}
	var firstErr error
	for _, fn := range r.cleanups {
		if err := fn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.cleanups = nil
	return firstErr
}

func (r *Resolver) build(ctx context.Context, b Binding) (action.AnyAction, func() error, error) {
	switch b.Kind {
	case KindHTTP:
		act, err := httpProxy(b)
		return act, nil, err
	case KindExec:
		act, err := execProxy(b)
		return act, nil, err
	case KindWASM:
		return wasmProxy(ctx, b)
	default:
		return nil, nil, fmt.Errorf("capability %s: unknown binding kind", b.Name)
	}
}

func parseTimeout(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
