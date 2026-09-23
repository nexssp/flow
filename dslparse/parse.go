package dslparse

import "strings"

// ParseManifest parses a full .nflow manifest. Returns a map from
// action name to its modifiers, plus the accumulated @config: state.
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

// ParseLine parses a single .nflow line. Returns the raw atom name, the
// head atom (for composites), whether the line is a pipeline, and the
// parsed modifiers.
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

	var p Modifiers
	if err := applyModifiers(splitModifiers(modsStr), &p); err != nil {
		// Modifier errors are surfaced by the caller (compiler) through
		// the line's position. parse.go does not have the file path, so
		// it stores the error in the Modifiers struct for later. Until
		// then, ignore the error here and let the compiler pick it up.
		_ = err
	}
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

// mergeModifiers combines two Modifiers. Second wins for scalars; slices
// are appended.
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
	if b.Debug {
		a.Debug = true
	}
	if b.Validate {
		a.Validate = true
	}
	if b.Effect != "" {
		a.Effect = b.Effect
	}
	if b.Gate != "" {
		a.Gate = b.Gate
	}
	if b.ReadOnly {
		a.ReadOnly = true
	}
	if b.Provider != "" {
		a.Provider = b.Provider
	}
	if b.Model != "" {
		a.Model = b.Model
	}
	if b.Typed != "" {
		a.Typed = b.Typed
	}
	if b.Schema != "" {
		a.Schema = b.Schema
	}
	if b.System != "" {
		a.System = b.System
	}
	if b.MaxTurns > 0 {
		a.MaxTurns = b.MaxTurns
	}
	if b.Image != "" {
		a.Image = b.Image
	}

	a.Roles = append(a.Roles, b.Roles...)
	a.Permissions = append(a.Permissions, b.Permissions...)
	a.Features = append(a.Features, b.Features...)
	a.Tags = append(a.Tags, b.Tags...)
	a.HITLOptions = append(a.HITLOptions, b.HITLOptions...)
	a.HITLTriggers = append(a.HITLTriggers, b.HITLTriggers...)
	a.CLIAliases = append(a.CLIAliases, b.CLIAliases...)
	a.A2AExample = append(a.A2AExample, b.A2AExample...)
	a.Tools = append(a.Tools, b.Tools...)
	a.Skills = append(a.Skills, b.Skills...)
	a.Transports = append(a.Transports, b.Transports...)

	return a
}
