package transport

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/nexssp/kernel/action"
)

type Factory[T any] func(context.Context, T) (action.Library, error)

type loader func(context.Context, map[string]string) (action.Library, error)

type registry struct {
	mu      sync.RWMutex
	loaders map[string]loader
}

var transports = registry{loaders: make(map[string]loader)}

func Register[T any](prefix string, factory Factory[T]) {
	if prefix == "" || factory == nil {
		panic("flow/transport: invalid library registration")
	}
	transports.mu.Lock()
	defer transports.mu.Unlock()
	if _, exists := transports.loaders[prefix]; exists {
		panic("flow/transport: duplicate library registration: " + prefix)
	}

	transports.loaders[prefix] = func(ctx context.Context, raw map[string]string) (action.Library, error) {
		cfg, err := Decode[T](ctx, raw)
		if err != nil {
			return action.Library{}, fmt.Errorf("flow/transport: %s: %w", prefix, err)
		}
		return factory(ctx, cfg)
	}
}

func Load(ctx context.Context, prefix string, raw map[string]string) (action.Library, error) {
	transports.mu.RLock()
	fn := transports.loaders[prefix]
	transports.mu.RUnlock()
	if fn == nil {
		return action.Library{}, fmt.Errorf("flow/transport: unknown library %q", prefix)
	}
	return fn(ctx, raw)
}

// KnownPrefixes returns sorted list of all registered transport prefixes
func KnownPrefixes() []string {
	transports.mu.RLock()
	defer transports.mu.RUnlock()
	out := make([]string, 0, len(transports.loaders))
	for name := range transports.loaders {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
