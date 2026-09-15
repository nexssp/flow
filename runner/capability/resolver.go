package capability

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type Resolver struct {
	registry *flow.MapRegistry
	bindings []Binding
	built    map[string]action.AnyAction
	cleanups []func() error
}

// NewResolver now takes ctx so WASM compilation can be cancelled.
func NewResolver(ctx context.Context, registry *flow.MapRegistry, manifest string) (*Resolver, error) {
	bindings := ParseBindings(manifest)

	r := &Resolver{
		registry: registry,
		bindings: bindings,
		built:    make(map[string]action.AnyAction, len(bindings)),
	}

	for _, b := range bindings {
		if _, ok := registry.Get(b.Name); ok {
			continue
		}

		act, cleanup, err := r.build(ctx, b)
		if err != nil {
			_ = r.Close()

			return nil, err
		}

		r.built[b.Name] = act
		if cleanup != nil {
			r.cleanups = append(r.cleanups, cleanup)
		}
	}

	return r, nil
}

func (r *Resolver) Resolve(name string) (action.AnyAction, bool) {
	if act, ok := r.registry.Get(name); ok {
		return act, true
	}

	if act, ok := r.built[name]; ok {
		return act, true
	}

	return nil, false
}

func (r *Resolver) Install() {
	for name, act := range r.built {
		r.registry.Register(name, act)
	}
}

func (r *Resolver) Bindings() []Binding { return r.bindings }

func (r *Resolver) Close() error {
	var firstErr error
	for _, fn := range r.cleanups {
		if err := fn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	r.cleanups = nil

	return firstErr
}

// build takes ctx and forwards it only to the wasm branch.
// httpProxy and execProxy do no eager I/O, so they don't need it.
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
