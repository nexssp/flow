package dslparse

import (
	"strings"
	"time"
)

// Modifiers is the full parse output for a single action line in a .flow
// manifest. Analyzer consumes every field; the runtime overlay uses a subset.
type Modifiers struct {
	CustomName  string
	Description string

	ReqType string
	ResType string

	Method string
	Path   string

	Transports []TransportBinding

	Timeout           time.Duration
	CacheTTL          time.Duration
	CacheKey          string
	RetryMax          int
	RetryPredicate    string
	BackoffStrategy   string
	BackoffBase       time.Duration
	BackoffMax        time.Duration
	Idempotent        bool
	IdempotencyHeader string
	RateLimit         string
	RateLimitRPS      float64
	RateLimitBurst    int
	ConcurrencyLimit  int32
	BreakerFailures   int
	BreakerCooldown   time.Duration
	Priority          string

	RequiresAuth bool
	Roles        []string
	Permissions  []string
	Features     []string

	Tags          []string
	Scope         string
	SuccessStatus int
	BudgetMicros  int64
	Audit         bool

	HITLPrompt   string
	HITLOptions  []string
	HITLTriggers []string

	Deprecated      bool
	DeprecatedSince string
	DeprecatedUse   string

	SSEChannel string
	CLIAliases []string
	CLIDesc    string
	A2ADesc    string
	A2AExample []string
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

// ParsedLine is the per-line parse result: the pipeline head atom (if any),
// whether the line is a composite pipeline, and the parsed modifiers for the
// tail atom.
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

// ParseManifest parses a full .flow manifest. Returns a map from action name
// to its modifiers, plus the accumulated @config: state.
func ParseManifest(content string) (map[string]Modifiers, Config) {
	out := make(map[string]Modifiers)
	cfg := Config{Silent: map[string]bool{}}

	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)

		if strings.HasPrefix(line, "@config:") {
			ApplyConfigLine(&cfg, line)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		pl, ok := ParseLine(line)
		if !ok {
			continue
		}
		name := pl.RawName
		if pl.Modifiers.CustomName != "" {
			name = pl.Modifiers.CustomName
		}
		if existing, ok := out[name]; ok {
			out[name] = mergeModifiers(existing, pl.Modifiers)
		} else {
			out[name] = pl.Modifiers
		}
	}
	return out, cfg
}

// ParseLine parses a single .flow line. Returns the raw atom name, the head
// atom (for composites), whether the line is a pipeline, and the parsed
// modifiers. Returns false when the line is empty or malformed.
func ParseLine(line string) (ParsedLine, bool) {
	targetAtom, isComposite := extractTargetAtomFromLine(line)
	if targetAtom == "" {
		return ParsedLine{}, false
	}

	colonIdx := strings.IndexByte(targetAtom, ':')
	rawName := targetAtom
	modsStr := ""
	if colonIdx >= 0 {
		rawName = strings.TrimSpace(targetAtom[:colonIdx])
		modsStr = targetAtom[colonIdx+1:]
	}
	if rawName == "" {
		return ParsedLine{}, false
	}

	p := parseModifiers(splitModifiers(modsStr))
	applyTransportExtras(&p)

	head := ""
	if isComposite {
		arrows := scanTopLevelArrows(line)
		if len(arrows) > 0 {
			head = strings.TrimSpace(line[:arrows[0]])
			if cIdx := strings.IndexByte(head, ':'); cIdx > 0 {
				head = head[:cIdx]
			}
		}
	}

	return ParsedLine{
		RawName:     rawName,
		HeadAtom:    head,
		IsComposite: isComposite,
		Modifiers:   p,
	}, true
}

// ApplyConfigLine parses one @config: directive into cfg.
func ApplyConfigLine(cfg *Config, line string) {
	body := strings.TrimPrefix(line, "@config:")
	kv := strings.SplitN(body, "=", 2)
	if len(kv) != 2 {
		return
	}
	k := strings.TrimSpace(kv[0])
	v := trimValue(kv[1])
	switch k {
	case "silent", "silent_rules", "mute":
		for _, r := range splitCSV(v) {
			cfg.Silent[r] = true
		}
	case "strict":
		cfg.Strict = strings.EqualFold(v, "true")
	}
}

func mergeModifiers(a, b Modifiers) Modifiers {
	if b.CustomName != "" {
		a.CustomName = b.CustomName
	}
	if b.Description != "" {
		a.Description = b.Description
	}
	if b.ReqType != "" {
		a.ReqType = b.ReqType
	}
	if b.ResType != "" {
		a.ResType = b.ResType
	}
	if b.Method != "" {
		a.Method = b.Method
	}
	if b.Path != "" {
		a.Path = b.Path
	}
	if b.Timeout > 0 {
		a.Timeout = b.Timeout
	}
	if b.CacheTTL > 0 {
		a.CacheTTL = b.CacheTTL
	}
	if b.CacheKey != "" {
		a.CacheKey = b.CacheKey
	}
	if b.RetryMax > 0 {
		a.RetryMax = b.RetryMax
	}
	if b.RetryPredicate != "" {
		a.RetryPredicate = b.RetryPredicate
	}
	if b.BackoffStrategy != "" {
		a.BackoffStrategy = b.BackoffStrategy
	}
	if b.BackoffBase > 0 {
		a.BackoffBase = b.BackoffBase
	}
	if b.BackoffMax > 0 {
		a.BackoffMax = b.BackoffMax
	}
	if b.Idempotent {
		a.Idempotent = true
	}
	if b.IdempotencyHeader != "" {
		a.IdempotencyHeader = b.IdempotencyHeader
	}
	if b.RateLimit != "" {
		a.RateLimit = b.RateLimit
	}
	if b.RateLimitRPS > 0 {
		a.RateLimitRPS = b.RateLimitRPS
	}
	if b.RateLimitBurst > 0 {
		a.RateLimitBurst = b.RateLimitBurst
	}
	if b.ConcurrencyLimit > 0 {
		a.ConcurrencyLimit = b.ConcurrencyLimit
	}
	if b.BreakerFailures > 0 {
		a.BreakerFailures = b.BreakerFailures
	}
	if b.BreakerCooldown > 0 {
		a.BreakerCooldown = b.BreakerCooldown
	}
	if b.Priority != "" {
		a.Priority = b.Priority
	}
	if b.RequiresAuth {
		a.RequiresAuth = true
	}
	if b.Scope != "" {
		a.Scope = b.Scope
	}
	if b.SuccessStatus > 0 {
		a.SuccessStatus = b.SuccessStatus
	}
	if b.BudgetMicros > 0 {
		a.BudgetMicros = b.BudgetMicros
	}
	if b.Audit {
		a.Audit = true
	}
	if b.HITLPrompt != "" {
		a.HITLPrompt = b.HITLPrompt
	}
	if b.Deprecated {
		a.Deprecated = true
	}
	if b.DeprecatedSince != "" {
		a.DeprecatedSince = b.DeprecatedSince
	}
	if b.DeprecatedUse != "" {
		a.DeprecatedUse = b.DeprecatedUse
	}
	if b.SSEChannel != "" {
		a.SSEChannel = b.SSEChannel
	}
	if b.CLIDesc != "" {
		a.CLIDesc = b.CLIDesc
	}
	if b.A2ADesc != "" {
		a.A2ADesc = b.A2ADesc
	}

	a.Roles = append(a.Roles, b.Roles...)
	a.Permissions = append(a.Permissions, b.Permissions...)
	a.Features = append(a.Features, b.Features...)
	a.Tags = append(a.Tags, b.Tags...)
	a.HITLOptions = append(a.HITLOptions, b.HITLOptions...)
	a.HITLTriggers = append(a.HITLTriggers, b.HITLTriggers...)
	a.CLIAliases = append(a.CLIAliases, b.CLIAliases...)
	a.A2AExample = append(a.A2AExample, b.A2AExample...)
	a.Transports = append(a.Transports, b.Transports...)

	return a
}

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
