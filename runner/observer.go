package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexssp/ai/llm"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xerr"
)

// maxObserverVerbosity is the highest verbosity level the observer honours.
// Values above this are clamped, not wrapped.
const maxObserverVerbosity = 3

func clampVerbosity(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > maxObserverVerbosity:
		return maxObserverVerbosity
	default:
		// n is bounded by the two cases above.
		return int32(n) //nolint:gosec // G115: bounded to [0, maxObserverVerbosity]
	}
}

type MetricRecord struct {
	ExecutionID   string
	Action        string
	Duration      time.Duration
	PromptSnippet string
	PromptTokens  int
	CompTokens    int
	CostMicros    int64
	CostKnown     bool
	Success       bool
}

type RunnerObserver struct {
	mu        sync.Mutex
	out       io.Writer
	verbosity atomic.Int32
	records   []MetricRecord
	spent     atomic.Int64
}

func NewRunnerObserver(out io.Writer, verbosity int) *RunnerObserver {
	if out == nil {
		out = io.Discard
	}

	o := &RunnerObserver{out: out}
	o.verbosity.Store(clampVerbosity(verbosity))

	return o
}

func (o *RunnerObserver) SetVerbosity(n int) {
	o.verbosity.Store(clampVerbosity(n))
}

func (o *RunnerObserver) Verbosity() int {
	return int(o.verbosity.Load())
}

func (o *RunnerObserver) Out() io.Writer { return o.out }

func (o *RunnerObserver) AddSpend(micros int64) {
	if micros > 0 {
		o.spent.Add(micros)
	}
}

func (o *RunnerObserver) ProviderTrace(
	kind, provider, model string,
	in, out int,
	costMicro int64,
	dur time.Duration,
	err error,
) {
	if o == nil || o.out == nil {
		return
	}

	tNow := time.Now().Format("15:04:05.000")

	switch kind {
	case "start":
		if o.Verbosity() >= 1 {
			fmt.Fprintf(o.out, "[%s] [PROVIDER  ] 🔌 %s:%s …\n", tNow, provider, model)
		}
	case "finish":
		if o.Verbosity() >= 1 {
			cost := "-"
			if costMicro > 0 || knownModel(model) {
				cost = fmt.Sprintf("$%.6f", float64(costMicro)/1_000_000.0)
			}

			fmt.Fprintf(o.out,
				"[%s] [PROVIDER  ] ✅ %s:%s → %d in / %d out / %s / %s\n",
				tNow, provider, model, in, out,
				cost, dur.Round(time.Millisecond))
		}
	case "warn":
		fmt.Fprintf(o.out,
			"[%s] [PROVIDER  ] ⚠️  %s:%s %v\n", tNow, provider, model, err)
	case "error":
		fmt.Fprintf(o.out,
			"[%s] [PROVIDER  ] ❌ %s:%s failed after %s: %v\n",
			tNow, provider, model, dur.Round(time.Millisecond), err)
	}
}

func (o *RunnerObserver) Hook() action.AnyHook {
	return action.AnyHook{
		Before: func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
			name := actionName(meta)
			if !isUserAction(name) {
				return ctx, nil
			}

			tag := getAgentTag(name)
			tNow := time.Now().Format("15:04:05.000")

			if o.Verbosity() >= 1 {
				fmt.Fprintf(o.out, "[%s] [%-10s] ▶ %s\n", tNow, tag, name)
			}

			if o.Verbosity() >= 2 {
				if p := extractPrompt(req); p != "" {
					fmt.Fprintf(o.out, "              ├── 📝 %s\n", snip(p, 120))
				}
			}

			return ctx, nil
		},

		OnExecuted: func(ctx context.Context, req, res any, _ error, meta *action.Meta) {
			name := actionName(meta)
			if !isUserAction(name) {
				return
			}

			pTok, cTok, costMicro, known := extractTokensAndCost(res)
			if known && costMicro > 0 {
				o.spent.Add(costMicro)
			}

			tag := getAgentTag(name)
			tNow := time.Now().Format("15:04:05.000")
			summary := getResultSummary(res)

			if o.Verbosity() >= 1 {
				costInfo := ""

				if o.Verbosity() >= 2 && (pTok > 0 || cTok > 0) {
					costStr := "-"
					if known {
						costStr = fmt.Sprintf("$%.6f", float64(costMicro)/1_000_000.0)
					}

					costInfo = fmt.Sprintf(" · tokens %d/%d · %s", pTok, cTok, costStr)
				}

				fmt.Fprintf(o.out, "[%s] [%-10s] ✅ %s%s\n", tNow, tag, summary, costInfo)
			}
		},

		OnRetry: func(ctx context.Context, req any, attempt int, err error, meta *action.Meta) {
			name := actionName(meta)
			if !isUserAction(name) {
				return
			}

			tag := getAgentTag(name)
			if tag == "SANDBOX" || tag == "CODER" {
				tag = "HEALER"
			}

			tNow := time.Now().Format("15:04:05.000")
			if o.Verbosity() >= 1 {
				fmt.Fprintf(o.out,
					"[%s] [%-10s] ↻ %s retry #%d: %s\n",
					tNow, tag, name, attempt, snip(err.Error(), 100))
			}
		},

		OnError: func(ctx context.Context, req any, err error, meta *action.Meta) {
			name := actionName(meta)
			if !isUserAction(name) {
				return
			}

			tag := getAgentTag(name)
			tNow := time.Now().Format("15:04:05.000")

			fmt.Fprintf(o.out,
				"[%s] [%-10s] ❌ %s failed: %s\n",
				tNow, tag, name, snip(err.Error(), 240))

			if o.Verbosity() >= 2 {
				var appErr *xerr.AppError
				if errors.As(err, &appErr) {
					fmt.Fprintf(o.out, "              ├── kind    : %s\n", appErr.Kind)
					fmt.Fprintf(o.out, "              ├── message : %s\n", appErr.Message)

					if appErr.Cause != nil {
						fmt.Fprintf(o.out, "              └── cause   : %v\n", appErr.Cause)
					}
				}
			}
		},
	}
}

func (o *RunnerObserver) Emit(_ context.Context, ev observe.Event) {
	if ev.Kind != observe.KindExecuted && ev.Kind != observe.KindError {
		return
	}

	if !isUserAction(ev.Action) {
		return
	}

	prompt := extractPrompt(ev.Request)
	pTok, cTok, cost, known := extractTokensAndCost(ev.Response)

	o.mu.Lock()
	o.records = append(o.records, MetricRecord{
		ExecutionID:   ev.ExecutionID,
		Action:        ev.Action,
		Duration:      ev.Duration,
		PromptSnippet: snip(prompt, 36),
		PromptTokens:  pTok,
		CompTokens:    cTok,
		CostMicros:    cost,
		CostKnown:     known,
		Success:       ev.Error == nil,
	})
	o.mu.Unlock()
}

func (o *RunnerObserver) TotalSpentMicros() int64 {
	return o.spent.Load()
}

func (o *RunnerObserver) TotalTokens() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	var total int
	for _, r := range o.records {
		total += r.PromptTokens + r.CompTokens
	}

	return total
}

func (o *RunnerObserver) PrintSummary(out io.Writer) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.records) == 0 {
		return
	}

	fmt.Fprintln(out, "\n📊 EXECUTION METRICS")
	fmt.Fprintln(out, strings.Repeat("─", 94))
	fmt.Fprintf(out, "%-4s │ %-24s │ %-10s │ %-16s │ %-20s │ %s\n",
		"ST", "ACTION", "DURATION", "TOKENS (IN/OUT)", "COST (USD)", "PROMPT")
	fmt.Fprintln(out, strings.Repeat("─", 94))

	var (
		totalCost  int64
		totalIn    int
		totalOut   int
		anyUnknown bool
	)

	for _, r := range o.records {
		status := "✅"
		if !r.Success {
			status = "❌"
		}

		tokStr := "-"
		if r.PromptTokens > 0 || r.CompTokens > 0 {
			tokStr = fmt.Sprintf("%d / %d", r.PromptTokens, r.CompTokens)
		}

		costStr := "-"

		switch {
		case r.CostKnown:
			costStr = fmt.Sprintf("$%.6f", float64(r.CostMicros)/1_000_000.0)
		case r.PromptTokens > 0 || r.CompTokens > 0:
			anyUnknown = true
		}

		fmt.Fprintf(out, "%s  │ %-24s │ %-10s │ %-16s │ %-20s │ %s\n",
			status, r.Action, r.Duration.Round(time.Millisecond),
			tokStr, costStr, r.PromptSnippet)

		totalCost += r.CostMicros
		totalIn += r.PromptTokens
		totalOut += r.CompTokens
	}

	fmt.Fprintln(out, strings.Repeat("─", 94))
	fmt.Fprintf(out, "💰 $%.6f USD · %d in / %d out\n",
		float64(totalCost)/1_000_000.0, totalIn, totalOut)

	if anyUnknown {
		fmt.Fprintln(out, "⚠️  some models have no rate card; cost shown as \"-\"")
	}
}

func isUserAction(name string) bool {
	return !strings.HasPrefix(name, "gate_") && !strings.HasPrefix(name, "system.")
}

func actionName(m *action.Meta) string {
	if m == nil {
		return ""
	}

	return m.Name
}

func knownModel(model string) bool {
	_, ok := llm.GlobalCatalog.Get(model)

	return ok
}

func snip(s string, maxLen int) string {
	s = strings.TrimSpace(s)

	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen || maxLen < 10 {
		return s
	}

	half := (maxLen - 3) / 2

	return s[:half] + "..." + s[len(s)-half:]
}
