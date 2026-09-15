package flow

import (
	"context"

	"github.com/nexssp/kernel/xctx"
)

// Layer identifies where a resolved config value came from. The runner
// prints the layer next to each value at -vvv so the operator can see
// whether a knob came from the CLI, the environment, the .flow file,
// or a built-in default.
type Layer uint8

const (
	LayerDefault Layer = iota
	LayerDSL
	LayerEnv
	LayerCLI
)

func (l Layer) String() string {
	switch l {
	case LayerCLI:
		return "cli"
	case LayerEnv:
		return "env"
	case LayerDSL:
		return "dsl"
	default:
		return "default"
	}
}

// Config is the complete set of runtime knobs for a flow run.
//
// Zero values are meaningful defaults supplied by defaultConfig. Every
// field is optional in the .flow, in the environment, and on the CLI.
type Config struct {
	Verbosity    int
	BudgetMicros int64
	Approval     string // "danger" | "all" | "none" — validated by the runner
	MaxTokens    int    // 0 = per-node default
	Observe      string // "live" | "json" | "off"
	Provider     string
	Model        string
	Sandbox      string
	OutputFormat string // "" | "json" | "text"
	OutputDir    string
}

// Resolved pairs the final Config with per-field provenance.
type Resolved struct {
	Config     Config
	Provenance map[string]Layer
}

// ResolveConfig applies CLI > env > DSL > default to every knob.
//
// cliArgs is the raw flag slice; the parser ignores anything that is
// not a config knob, so it is safe to pass the same args the runner
// also uses for runner-specific flags (-i, --assert=, …).
func ResolveConfig(dslText string, cliArgs []string) Resolved {
	prov := make(map[string]Layer, 10)
	cfg := defaultConfig()

	markAllDefaults(prov)

	applyDSL(&cfg, parseDSLConfig(dslText), prov)
	applyEnv(&cfg, readEnvConfig(), prov)
	applyCLI(&cfg, parseCLIConfig(cliArgs), prov)

	return Resolved{Config: cfg, Provenance: prov}
}

func defaultConfig() Config {
	return Config{
		Verbosity:    0,
		BudgetMicros: 10_000_000, // $10
		Approval:     "danger",
		MaxTokens:    0,
		Observe:      "live",
		OutputDir:    ".runs",
	}
}

// knobKeys is the canonical list of provenance keys. Kept in one place
// so adding a knob means editing this slice and the three appliers.
var knobKeys = []string{
	"verbosity", "budget", "approval", "max_tokens",
	"observe", "provider", "model", "sandbox",
	"output_format", "output_dir",
}

func markAllDefaults(prov map[string]Layer) {
	for _, k := range knobKeys {
		prov[k] = LayerDefault
	}
}

// ─────────────────────────────────────────────────────────────────────
// Per-run token budget
//
// WithMaxTokens stores a per-run MaxTokens override in the context.
// Nodes read it with MaxTokensFromCtx and fall back to their own
// default when it is absent or non-positive.
//
// The value is domainless: any node that wants a token budget can read
// it. The AI nodes happen to be the ones that do today.
// ─────────────────────────────────────────────────────────────────────

var maxTokensKey = xctx.NewKey[int]("flow.max_tokens")

func WithMaxTokens(ctx context.Context, n int) context.Context {
	if n <= 0 {
		return ctx
	}

	return maxTokensKey.With(ctx, n)
}

func MaxTokensFromCtx(ctx context.Context, def int) int {
	if ctx == nil {
		return def
	}

	if v, ok := maxTokensKey.From(ctx); ok && v > 0 {
		return v
	}

	return def
}
