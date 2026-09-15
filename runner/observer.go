package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexssp/cost"
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

// MetricRecord is one row in the metrics table printed at the end of a
// run, or at exit on failure.
type MetricRecord struct {
	ExecutionID   string
	Action        string
	Duration      time.Duration
	PromptSnippet string
	PromptTokens  int
	CompTokens    int
	CostMicros    int64
	Currency      cost.Currency
	CostKnown     bool
	Success       bool
}

// RunnerObserver is the flow runner's terminal reporter. It receives
// lifecycle events from two sources:
//
//   - the observe.Hook attached to every action by the compiler, which
//     feeds the end-of-run metrics table;
//   - the live hook returned by Hook(), which prints the per-action
//     trace lines while the flow is running.
//
// Domain-specific rendering (LLM token counts, evaluator verdicts,
// price catalog lookups) is delegated to ObserverHooks. A nil hook
// falls back to a domainless default, so the observer works for any
// flow — AI-flavoured or not.
type RunnerObserver struct {
	mu        sync.Mutex
	out       io.Writer
	verbosity atomic.Int32
	records   []MetricRecord
	spent     atomic.Int64
	hooks     ObserverHooks
}

// NewRunnerObserver creates an observer with no domain-specific hooks.
// Use this for flows that carry no AI/LLM actions.
func NewRunnerObserver(out io.Writer, verbosity int) *RunnerObserver {
	return NewRunnerObserverWithHooks(out, verbosity, ObserverHooks{})
}

// NewRunnerObserverWithHooks creates an observer that delegates
// domain-specific rendering to the supplied hooks. Nil hook functions
// are safe: the observer falls back to domainless defaults.
func NewRunnerObserverWithHooks(out io.Writer, verbosity int, hooks ObserverHooks) *RunnerObserver {
	if out == nil {
		out = io.Discard
	}

	o := &RunnerObserver{out: out, hooks: hooks}
	o.verbosity.Store(clampVerbosity(verbosity))

	return o
}

func (o *RunnerObserver) SetVerbosity(n int) { o.verbosity.Store(clampVerbosity(n)) }
func (o *RunnerObserver) Verbosity() int     { return int(o.verbosity.Load()) }
func (o *RunnerObserver) Out() io.Writer     { return o.out }

func (o *RunnerObserver) AddSpend(micros int64) {
	if micros > 0 {
		o.spent.Add(micros)
	}
}

// ─── hook dispatch: chained, so the AI hook gets first crack and the
//     domainless default catches everything else ──────────────────────

func (o *RunnerObserver) knownModel(model string) bool {
	if o.hooks.KnownModel == nil {
		return false
	}

	return o.hooks.KnownModel(model)
}

func (o *RunnerObserver) tokensAndCost(res any) (int, int, int64, cost.Currency, bool) {
	if o.hooks.TokensAndCost != nil {
		if in, out, micros, curr, known := o.hooks.TokensAndCost(res); known {
			return in, out, micros, curr, true
		}
	}

	return extractTokensAndCostGeneric(res)
}

func (o *RunnerObserver) resultSummary(res any) string {
	if o.hooks.ResultSummary != nil {
		if s := o.hooks.ResultSummary(res); s != "" {
			return s
		}
	}

	return "Task completed."
}

func (o *RunnerObserver) promptFromRequest(req any) string {
	if o.hooks.PromptFromRequest != nil {
		if p := o.hooks.PromptFromRequest(req); p != "" {
			return p
		}
	}

	return extractPromptGeneric(req)
}

// ─── provider trace: called by llm.NewObservableProvider ────────────

// ProviderTrace is the callback installed on the LLM provider wrapper.
// It receives one event per Complete call (start / finish / warn /
// error) and renders it to the live log at verbosity >= 1.
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
			costStr := "-"
			if costMicro > 0 || o.knownModel(model) {
				costStr = formatCost(costMicro, cost.USD)
			}

			fmt.Fprintf(o.out,
				"[%s] [PROVIDER  ] ✅ %s:%s → %d in / %d out / %s / %s\n",
				tNow, provider, model, in, out,
				costStr, dur.Round(time.Millisecond))
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

// ─── live hook: the per-action trace printed while the flow runs ────

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
				if p := o.promptFromRequest(req); p != "" {
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

			pTok, cTok, costMicro, curr, known := o.tokensAndCost(res)
			if known && costMicro > 0 {
				o.spent.Add(costMicro)
			}

			tag := getAgentTag(name)
			tNow := time.Now().Format("15:04:05.000")
			summary := o.resultSummary(res)

			if o.Verbosity() >= 1 {
				costInfo := ""

				if o.Verbosity() >= 2 && (pTok > 0 || cTok > 0) {
					costStr := "-"
					if known {
						costStr = formatCost(costMicro, curr)
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

// ─── metrics sink: called by observe.Hook attached to every action ──

func (o *RunnerObserver) Emit(_ context.Context, ev observe.Event) {
	if ev.Kind != observe.KindExecuted && ev.Kind != observe.KindError {
		return
	}

	if !isUserAction(ev.Action) {
		return
	}

	prompt := o.promptFromRequest(ev.Request)
	pTok, cTok, micros, curr, known := o.tokensAndCost(ev.Response)

	o.mu.Lock()
	o.records = append(o.records, MetricRecord{
		ExecutionID:   ev.ExecutionID,
		Action:        ev.Action,
		Duration:      ev.Duration,
		PromptSnippet: snip(prompt, 36),
		PromptTokens:  pTok,
		CompTokens:    cTok,
		CostMicros:    micros,
		Currency:      curr,
		CostKnown:     known,
		Success:       ev.Error == nil,
	})
	o.mu.Unlock()
}

func (o *RunnerObserver) TotalSpentMicros() int64 { return o.spent.Load() }

func (o *RunnerObserver) TotalTokens() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	var total int
	for _, r := range o.records {
		total += r.PromptTokens + r.CompTokens
	}

	return total
}

// PrintSummary renders the end-of-run metrics table. Callers pass the
// writer they want the table on (usually the same stdout the live log
// went to). Safe to call after the flow has failed.
func (o *RunnerObserver) PrintSummary(out io.Writer) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.records) == 0 {
		return
	}

	fmt.Fprintln(out, "\n📊 EXECUTION METRICS")
	fmt.Fprintln(out, strings.Repeat("─", 94))
	fmt.Fprintf(out, "%-4s │ %-24s │ %-10s │ %-16s │ %-20s │ %s\n",
		"ST", "ACTION", "DURATION", "TOKENS (IN/OUT)", "COST", "PROMPT")
	fmt.Fprintln(out, strings.Repeat("─", 94))

	var (
		totalIn    int
		totalOut   int
		anyUnknown bool
		byCurrency = map[cost.Currency]int64{}
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
			costStr = formatCost(r.CostMicros, r.Currency)
			byCurrency[r.Currency] += r.CostMicros
		case r.PromptTokens > 0 || r.CompTokens > 0:
			anyUnknown = true
		}

		fmt.Fprintf(out, "%s  │ %-24s │ %-10s │ %-16s │ %-20s │ %s\n",
			status, r.Action, r.Duration.Round(time.Millisecond),
			tokStr, costStr, r.PromptSnippet)

		totalIn += r.PromptTokens
		totalOut += r.CompTokens
	}

	fmt.Fprintln(out, strings.Repeat("─", 94))

	// One total line per currency seen. In the common case there is
	// exactly one currency and this prints a single line. Mixed-currency
	// runs happen when a flow reaches providers priced in different
	// currencies; the totals are kept separate rather than summed,
	// because summing across currencies requires an exchange rate the
	// observer does not have.
	currencies := make([]cost.Currency, 0, len(byCurrency))
	for c := range byCurrency {
		currencies = append(currencies, c)
	}
	sort.Slice(currencies, func(i, j int) bool {
		return currencies[i].String() < currencies[j].String()
	})

	for _, c := range currencies {
		fmt.Fprintf(out, "💰 %s · %d in / %d out\n",
			formatCost(byCurrency[c], c), totalIn, totalOut)
	}

	if anyUnknown {
		fmt.Fprintln(out, "⚠️  some results reported tokens without a price; cost shown as \"-\"")
	}
}

// ─── small helpers ──────────────────────────────────────────────────

func isUserAction(name string) bool {
	return !strings.HasPrefix(name, "gate_") && !strings.HasPrefix(name, "system.")
}

func actionName(m *action.Meta) string {
	if m == nil {
		return ""
	}

	return m.Name
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

// formatCost renders a micros amount with its currency code, using the
// currency's own String(). Never uses a hardcoded symbol table, so the
// observer is not tied to any particular currency.
func formatCost(micros int64, c cost.Currency) string {
	return fmt.Sprintf("%.6f %s", cost.Micro(micros).Float64(), c.String())
}
