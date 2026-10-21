package compiler

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

// PreprocessEvent describes one successfully preprocessed source fragment.
// Events are synchronous and ordered children before parents. A diamond can
// report the same file twice because each include is a distinct evaluation.
// Observers must not mutate Metadata. Source includes substitutions performed
// by directives; Original preserves the input, and Clean is the executable body.
// Directives contains only handlers actually invoked in this fragment, not
// directive-like text in comments, strings or a captured declaration body.
type PreprocessEvent struct {
	File       string
	Original   string
	Source     string
	Clean      string
	Metadata   map[string]any
	Directives []string
}

// PreprocessObserver observes the existing directive lifecycle without
// replacing source loading, parsing directives or changing their execution.
type PreprocessObserver func(PreprocessEvent)

var preprocessObserverKey = xctx.NewKey[PreprocessObserver]("flow.preprocess.observer")

// WithPreprocessObserver adds a synchronous observer to the current context.
// Nested observers are composed, so an inventory does not suppress diagnostics
// already installed by its caller. A nil observer leaves ctx unchanged.
func WithPreprocessObserver(ctx context.Context, observer PreprocessObserver) context.Context {
	if observer == nil {
		return ctx
	}
	if previous, ok := preprocessObserverKey.From(ctx); ok && previous != nil {
		return preprocessObserverKey.With(ctx, func(event PreprocessEvent) {
			previous(event)
			observer(event)
		})
	}
	return preprocessObserverKey.With(ctx, observer)
}

func observePreprocess(ctx context.Context, event PreprocessEvent) {
	if observer, ok := preprocessObserverKey.From(ctx); ok && observer != nil {
		observer(event)
	}
}
