package dslparse

import (
	"strings"
	"time"
)

// Modifiers is the full parse output for a single action line in a
// .nflow manifest. Analyzer consumes every field; the runtime overlay
// uses a subset.
type Modifiers struct {
	// ─── identity ───────────────────────────────────────────────────
	CustomName  string
	Description string
	ReqType     string
	ResType     string
	Method      string
	Path        string

	// ─── transports ─────────────────────────────────────────────────
	Transports []TransportBinding

	// ─── resilience ─────────────────────────────────────────────────
	Timeout          time.Duration
	RetryMax         int
	RetryPredicate   string
	BackoffStrategy  string
	BackoffBase      time.Duration
	BackoffMax       time.Duration
	BreakerFailures  int
	BreakerCooldown  time.Duration
	Priority         string
	ConcurrencyLimit int32
	RateLimit        string
	RateLimitRPS     float64
	RateLimitBurst   int

	// ─── cache / dedup ──────────────────────────────────────────────
	CacheTTL          time.Duration
	CacheKey          string
	Idempotent        bool
	IdempotencyHeader string

	// ─── security ───────────────────────────────────────────────────
	RequiresAuth  bool
	Roles         []string
	Permissions   []string
	Features      []string
	Tags          []string
	Scope         string
	SuccessStatus int
	BudgetMicros  int64
	Audit         bool

	// ─── HITL ───────────────────────────────────────────────────────
	HITLPrompt   string
	HITLOptions  []string
	HITLTriggers []string

	// ─── deprecation ────────────────────────────────────────────────
	Deprecated      bool
	DeprecatedSince string
	DeprecatedUse   string

	// ─── meta ───────────────────────────────────────────────────────
	Debug    bool
	Validate bool

	// ─── transports (extras) ────────────────────────────────────────
	SSEChannel string
	CLIAliases []string
	CLIDesc    string
	A2ADesc    string
	A2AExample []string

	// ─── effects / gates ────────────────────────────────────────────
	Effect   string // read_only | side_effect | high_risk
	Gate     string // condition for approval
	ReadOnly bool

	// ─── domain: LLM ────────────────────────────────────────────────
	Provider string
	Model    string
	Typed    string
	Schema   string
	System   string
	Tools    []string
	MaxTurns int

	// ─── domain: sandbox ────────────────────────────────────────────
	Image  string
	Skills []string

	HookNames []string
}

// TransportBinding mirrors the kernel-agnostic shape of a routing entry.
type TransportBinding struct {
	Kind       string            `json:"kind"`
	Protocol   string            `json:"protocol,omitempty"`
	Target     string            `json:"target"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Subject    string            `json:"subject,omitempty"`
	QueueGroup string            `json:"queue_group,omitempty"`
	Aliases    []string          `json:"aliases,omitempty"`
	Interval   time.Duration     `json:"interval,omitempty"`
	Schedule   string            `json:"schedule,omitempty"`
	Stream     bool              `json:"stream,omitempty"`
	Raw        bool              `json:"raw,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// ParsedLine is the per-line parse result.
type ParsedLine struct {
	RawName     string
	HeadAtom    string
	IsComposite bool
	Modifiers   Modifiers
}

// Config carries @config: directives collected during parse.
type Config struct {
	Silent map[string]bool
	Strict bool
}

// trimValue and splitCSV are shared by every mod_*.go file.
func trimValue(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func splitCSV(s string) []string {
	s = trimValue(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = trimValue(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
