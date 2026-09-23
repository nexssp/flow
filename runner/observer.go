package runner

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexssp/cost"
	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xerr"
)

const maxObserverVerbosity = 3

func clampVerbosity(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > maxObserverVerbosity:
		return maxObserverVerbosity
	default:
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
	Currency      cost.Currency
	CostKnown     bool
	Success       bool
}

type RunnerObserver struct {
	mu        sync.Mutex
	out       io.Writer
	verbosity atomic.Int32
	records   []MetricRecord
	spent     atomic.Int64
	hooks     ObserverHooks
}

func NewRunnerObserver(out io.Writer, verbosity int) *RunnerObserver {
	return NewRunnerObserverWithHooks(out, verbosity, ObserverHooks{})
}

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

// OnSecurity implements flow.SecurityObserver. It is called by the
// compiler for every profile hook that fires around a node. At
// verbosity >= 1, a passing hook prints one line; a rejecting hook
// prints a distinct failure line. At verbosity < 1, no output is
// produced — the events still fire, they are simply not rendered.
func (o *RunnerObserver) OnSecurity(_ context.Context, ev flow.SecurityEvent) {
	if o == nil || o.out == nil {
		return
	}

	v := o.Verbosity()
	if v < 1 {
		return
	}

	// "after" events are only interesting when they carry an error;
	// the matching "before" line already showed the hook firing.
	if ev.Phase == "after" && ev.Err == nil {
		return
	}

	tNow := time.Now().Format("15:04:05.000")

	switch {
	case ev.Err == nil:
		fmt.Fprintf(o.out,
			"[%s] [SECURITY  ] 🛡️  %-26s node=%-20s pass (%s)\n",
			tNow, ev.Hook, ev.Node, ev.Elapsed.Round(time.Microsecond),
		)
	case ev.Phase == "error":
		fmt.Fprintf(o.out,
			"[%s] [SECURITY  ] ❌ %-26s node=%-20s fail: %s\n",
			tNow, ev.Hook, ev.Node, snip(ev.Err.Error(), 160),
		)
	default:
		fmt.Fprintf(o.out,
			"[%s] [SECURITY  ] ⛔ %-26s node=%-20s blocked: %s\n",
			tNow, ev.Hook, ev.Node, snip(ev.Err.Error(), 160),
		)
	}
}

// ─── hook dispatch ─────────────────────────────────────────────────

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

// ─── provider trace ────────────────────────────────────────────────

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

// ─── live hook ─────────────────────────────────────────────────────

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

		OnSuccess: func(ctx context.Context, req, res any, meta *action.Meta) {
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
			if o.Verbosity() < 1 {
				return
			}

			tag := getAgentTag(name)
			tNow := time.Now().Format("15:04:05.000")
			appErr := xerr.From(err)

			fmt.Fprintf(o.out, "[%s] [%-10s] ✗ %s: %s\n", tNow, tag, name, appErr.Message)

			if o.Verbosity() >= 2 {
				fmt.Fprintf(o.out, "              ├── kind    : %s\n", appErr.Kind)
				if appErr.Cause != nil {
					fmt.Fprintf(o.out, "              └── cause   : %v\n", appErr.Cause)
				}
			}
		},
	}
}

// ─── metrics sink ──────────────────────────────────────────────────

func (o *RunnerObserver) Emit(_ context.Context, ev observe.Event) {
	if ev.Kind != observe.KindSuccess && ev.Kind != observe.KindError {
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

// ─── helpers ───────────────────────────────────────────────────────

func isUserAction(name string) bool {
	switch {
	case name == "graph.execute":
		return false
	case strings.HasPrefix(name, "gate_"):
		return false
	case strings.HasPrefix(name, "system."):
		return false
	}
	return true
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

func formatCost(micros int64, c cost.Currency) string {
	return fmt.Sprintf("%.6f %s", cost.Micro(micros).Float64(), c.String())
}
