package core

import (
	"context"
	"sort"
	"sync"

	"github.com/nexssp/kernel/action"
)

// BoundarySpec is a stream-to-unary adapter. It is the only place in
// a pipeline where iter.Seq2[T, error] becomes a single value.
type BoundarySpec struct {
	Name        string
	Description string
	Consume     func(stream action.AnyStreamAction) *action.Builder[any, any]
}

var (
	boundaryMu       sync.RWMutex
	boundaryRegistry = map[string]BoundarySpec{}
)

func RegisterBoundary(spec BoundarySpec) {
	if spec.Name == "" {
		panic("core: RegisterBoundary with empty Name")
	}
	if spec.Consume == nil {
		panic("core: RegisterBoundary(" + spec.Name + ") with nil Consume")
	}
	boundaryMu.Lock()
	defer boundaryMu.Unlock()
	if _, dup := boundaryRegistry[spec.Name]; dup {
		panic("core: duplicate boundary " + spec.Name)
	}
	boundaryRegistry[spec.Name] = spec
}

func BoundaryByName(name string) (BoundarySpec, bool) {
	boundaryMu.RLock()
	defer boundaryMu.RUnlock()
	s, ok := boundaryRegistry[name]
	return s, ok
}

func NamedBoundaries() []BoundarySpec {
	boundaryMu.RLock()
	defer boundaryMu.RUnlock()
	out := make([]BoundarySpec, 0, len(boundaryRegistry))
	for _, s := range boundaryRegistry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func init() {
	RegisterBoundary(BoundarySpec{
		Name:        "collect",
		Description: "Collect every stream item into []any",
		Consume: func(stream action.AnyStreamAction) *action.Builder[any, any] {
			return action.New("collect", func(ctx context.Context, req any) (any, error) {
				seq, err := stream.DoStreamAny(ctx, req)
				if err != nil {
					return nil, err
				}
				items := make([]any, 0, 8)
				var itemErr error
				seq(func(item any, err error) bool {
					if err != nil {
						itemErr = err
						return false
					}
					items = append(items, item)
					return true
				})
				if itemErr != nil {
					return nil, itemErr
				}
				return items, nil
			}).Description("Collect every stream item into []any")
		},
	})
}
