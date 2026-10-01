package runtime

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// Noop returns the input unchanged. It is the identity action used in
// tests, as a placeholder in pipeline composition, and as an explicit
// terminator for a stream.
var Noop = action.New("noop", func(_ context.Context, in any) (any, error) {
	return in, nil
}).Description("Pass input through unchanged").
	Tag("base", "identity").
	Build()
