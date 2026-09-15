package runner

import (
	"time"

	"github.com/nexssp/cost"
)

// tokenCostReporter is the domainless shape a result can implement to
// report its own token count and cost. Micros are currency-agnostic;
// the currency itself is reported separately through currencyReporter.
//
// A result that reports tokens but no cost is fine: return 0 for
// micros and the observer renders the cost column as "-".
type tokenCostReporter interface {
	Tokens() (int, int)
	CostMicros() int64
}

// currencyReporter is optional. When absent, the observer assumes USD,
// matching cost.USD which is the default in cost.NewLedger.
type currencyReporter interface {
	Currency() cost.Currency
}

// timeNow is a swappable clock for tests.
var timeNow = time.Now

// extractPromptGeneric returns a prompt-like string from a request.
// It understands the two domainless shapes every non-AI caller uses:
// a map with "prompt" or "goal", or anything else (returns empty).
func extractPromptGeneric(req any) string {
	if req == nil {
		return ""
	}

	if m, ok := req.(map[string]any); ok {
		if p, ok := m["prompt"].(string); ok && p != "" {
			return p
		}

		if g, ok := m["goal"].(string); ok && g != "" {
			return g
		}
	}

	return ""
}

// extractTokensAndCostGeneric is the domainless fallback. It returns
// (prompt, completion, micros, currency, known).
func extractTokensAndCostGeneric(res any) (int, int, int64, cost.Currency, bool) {
	if res == nil {
		return 0, 0, 0, cost.USD, false
	}

	r, ok := res.(tokenCostReporter)
	if !ok {
		return 0, 0, 0, cost.USD, false
	}

	in, out := r.Tokens()

	curr := cost.USD
	if c, ok := res.(currencyReporter); ok {
		curr = c.Currency()
	}

	return in, out, r.CostMicros(), curr, true
}
