package runtime

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// With merges the @{...} args into the input map and passes the result
// downstream unchanged. Args are injected by the compiler before the
// handler runs, so the handler is a plain pass-through:
//
//	{ goal: "x", attempt: 1 } -> with @{ attempt: .attempt + 1 }
//	// → { goal: "x", attempt: 2 }
//
// Called without @{...} it degenerates to noop. Semantics match C#
// record `with { ... }`: the input is the base, listed fields win.
var With = action.New("with", func(_ context.Context, in any) (any, error) {
	return in, nil
}).Description("Merge @{...} args into the input map").
	Tag("base", "shape").
	Build()
