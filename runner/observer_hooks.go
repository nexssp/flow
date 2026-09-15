package runner

import "github.com/nexssp/cost"

// ObserverHooks lets a domain layer teach the observer how to render
// results it understands. Every hook is optional: nil functions fall
// through to the domainless defaults in observer_extract.go.
type ObserverHooks struct {
	// KnownModel reports whether model is in the local price catalog.
	KnownModel func(model string) bool

	// TokensAndCost extracts (prompt, completion, cost_micros, currency,
	// known) from an arbitrary action result.
	TokensAndCost func(res any) (int, int, int64, cost.Currency, bool)

	// ResultSummary renders a short human-readable summary of a result.
	ResultSummary func(res any) string

	// PromptFromRequest extracts a display prompt from a request. When
	// nil, only maps with "prompt" or "goal" are recognized.
	PromptFromRequest func(req any) string
}
