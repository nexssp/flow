package runner

import (
	"time"

	"github.com/nexssp/ai/llm"
)

type tokenCostReporter interface {
	Tokens() (int, int)
	CostUSD() int64
}

// timeNow is a swappable clock for tests. Assigning time.Now directly
// (instead of wrapping it in a lambda) keeps the compiler happy and
// silences gocritic's unlambda check, while remaining test-overridable.
var timeNow = time.Now

func extractPrompt(req any) string {
	if req == nil {
		return ""
	}

	if llmReq, ok := req.(llm.Request); ok && len(llmReq.Messages) > 0 {
		return llmReq.Messages[len(llmReq.Messages)-1].Content
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

func extractTokensAndCost(res any) (int, int, int64, bool) {
	if res == nil {
		return 0, 0, 0, false
	}

	if r, ok := res.(*llm.Response); ok && r != nil {
		_, _, known := r.Cost()

		return r.PromptTokens, r.CompTokens, int64(r.CostMicros()), known
	}

	if r, ok := res.(llm.Response); ok {
		_, _, known := r.Cost()

		return r.PromptTokens, r.CompTokens, int64(r.CostMicros()), known
	}

	if u, ok := res.(llm.Usage); ok {
		_, _, known := llm.CalculateCost(
			u.Model, u.PromptTokens, u.CachedPromptTokens, u.CompTokens, timeNow())

		return u.PromptTokens, u.CompTokens, u.CostUSD(), known
	}

	if u, ok := res.(*llm.Usage); ok && u != nil {
		_, _, known := llm.CalculateCost(
			u.Model, u.PromptTokens, u.CachedPromptTokens, u.CompTokens, timeNow())

		return u.PromptTokens, u.CompTokens, u.CostUSD(), known
	}

	if r, ok := res.(tokenCostReporter); ok {
		in, out := r.Tokens()

		return in, out, r.CostUSD(), true
	}

	return 0, 0, 0, false
}
