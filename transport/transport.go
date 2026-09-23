package transport

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/nexssp/flow/dslparse"
	"github.com/nexssp/kernel/action"
)

// ResolverFunc translates one DSL binding kind into an action.Binding.
type ResolverFunc func(*dslparse.TransportBinding) (action.Binding, error)

// ProviderFunc opens a runtime transport for `nexssflow serve`.
type ProviderFunc func(ctx context.Context, cfg map[string]string) (Service, error)

// Service is a live transport instance.
type Service interface {
	Serve(ctx context.Context, pipeline action.AnyAction, binding *dslparse.TransportBinding) error
	Close() error
}

var (
	mu        sync.RWMutex
	resolvers = map[string]ResolverFunc{}
	providers = map[string]ProviderFunc{}
)

// RegisterResolverMap binds every kind in the map. Panics on a
// duplicate kind: a misconfigured ecosystem must fail at process
// start, not at run time.
func RegisterResolverMap(m map[string]ResolverFunc) {
	mu.Lock()
	defer mu.Unlock()
	for kind, fn := range m {
		if kind == "" || fn == nil {
			panic("flow/transport: invalid resolver entry")
		}
		if _, dup := resolvers[kind]; dup {
			panic("flow/transport: duplicate kind " + kind)
		}
		resolvers[kind] = fn
	}
}

// RegisterProvider registers a runtime provider under a prefix.
func RegisterProvider(prefix string, fn ProviderFunc) {
	if prefix == "" || fn == nil {
		panic("flow/transport: invalid provider entry")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := providers[prefix]; dup {
		panic("flow/transport: duplicate provider " + prefix)
	}
	providers[prefix] = fn
}

// Resolve translates a DSL binding into a runtime Binding.
func Resolve(b *dslparse.TransportBinding) (action.Binding, bool, error) {
	if b == nil || b.Kind == "" {
		return nil, false, nil
	}
	mu.RLock()
	fn, ok := resolvers[b.Kind]
	mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	out, err := fn(b)
	if err != nil {
		return nil, true, fmt.Errorf("flow/transport: kind %q: %w", b.Kind, err)
	}
	return out, true, nil
}

func HasProvider(prefix string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := providers[prefix]
	return ok
}

func ProviderByPrefix(prefix string) (ProviderFunc, bool) {
	mu.RLock()
	defer mu.RUnlock()
	fn, ok := providers[prefix]
	return fn, ok
}

func KnownKinds() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(resolvers))
	for k := range resolvers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func KnownPrefixes() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(providers))
	for p := range providers {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// RegisterStateless validates options against T and returns an empty
// library. Transports without actions of their own (HTTP, CLI, cron,
// worker, topic) call this from their Register.
func RegisterStateless[T any](ctx context.Context, name string, raw map[string]string) (action.Library, error) {
	_, err := Decode[T](ctx, raw)
	return action.Library{Name: name}, err
}

// ServeOne mounts a single pipeline on the given binding and blocks.
// Providers call this from their Serve method.
func ServeOne(
	ctx context.Context,
	tr interface {
		Mount([]action.AnyAction)
		Do(context.Context, any) (any, error)
	},
	pipeline action.AnyAction,
	binding *dslparse.TransportBinding,
) error {
	route, handled, err := Resolve(binding)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("flow/transport: no resolver for kind %q", binding.Kind)
	}
	bound := action.Dynamic(pipeline).Route(route).Build()
	tr.Mount([]action.AnyAction{bound})
	_, err = tr.Do(ctx, nil)
	return err
}

// flow/transport/transport.go (additions)

// TriggerResolver converts an @on event declaration into the
// TransportBinding that the transport's own Resolver will later
// convert into an action.Binding.
//
// Each transport library registers exactly one, and owns the grammar
// of its own protocol strings.
type TriggerResolver interface {
	Handles(protocol string) bool
	Resolve(protocol, target string) (*dslparse.TransportBinding, error)
}

var (
	triggerMu        sync.RWMutex
	triggerResolvers []TriggerResolver
)

func RegisterTriggerResolver(r TriggerResolver) {
	if r == nil {
		panic("flow/transport: RegisterTriggerResolver(nil)")
	}
	triggerMu.Lock()
	defer triggerMu.Unlock()
	triggerResolvers = append(triggerResolvers, r)
}

func ResolveEventTrigger(protocol, target string) (*dslparse.TransportBinding, error) {
	triggerMu.RLock()
	defer triggerMu.RUnlock()

	for _, r := range triggerResolvers {
		if r.Handles(protocol) {
			return r.Resolve(protocol, target)
		}
	}
	return nil, fmt.Errorf(
		"flow/transport: no trigger resolver for protocol %q (registered: %v)",
		protocol, KnownTriggerProtocols(),
	)
}

func KnownTriggerProtocols() []string {
	/* iterate resolvers */
	return []string{}
}
