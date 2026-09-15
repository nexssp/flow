package dslparse

import (
	"strconv"
	"strings"
	"time"
)

func parseModifiers(mods []string) Modifiers {
	var p Modifiers

	for _, m := range mods {
		mClean := strings.Trim(m, `"' `)
		mLower := strings.ToLower(mClean)

		switch {
		case strings.HasPrefix(mLower, "route="):
			p.Method, p.Path = parseRouteValue(mClean[len("route="):], "POST")
		case strings.HasPrefix(mLower, "http="):
			method, path := parseRouteValue(mClean[len("http="):], "POST")
			p.Method, p.Path = method, path
			if path != "" {
				p.Transports = append(p.Transports, TransportBinding{
					Kind: "http", Protocol: "thttp",
					Target: method + " " + path,
					Method: method, Path: path,
				})
			}

		case strings.HasPrefix(mLower, "name="):
			p.CustomName = trimValue(mClean[len("name="):])
		case strings.HasPrefix(mLower, "desc=") || strings.HasPrefix(mLower, "description="):
			val := strings.TrimPrefix(mClean, "desc=")
			val = strings.TrimPrefix(val, "description=")
			p.Description = trimValue(val)

		case strings.HasPrefix(mLower, "type="):
			val := trimValue(mClean[len("type="):])
			if arrow := strings.Index(val, "->"); arrow >= 0 {
				p.ReqType = strings.TrimSpace(val[:arrow])
				p.ResType = strings.TrimSpace(val[arrow+2:])
			} else if val != "" {
				p.ReqType = val
			}

		case strings.HasPrefix(mLower, "timeout="):
			if d, err := time.ParseDuration(strings.TrimPrefix(mLower, "timeout=")); err == nil {
				p.Timeout = d
			}
		case strings.HasPrefix(mLower, "cache="):
			if d, err := time.ParseDuration(strings.TrimPrefix(mLower, "cache=")); err == nil {
				p.CacheTTL = d
			}
		case strings.HasPrefix(mLower, "cache_key="):
			p.CacheKey = trimValue(mClean[len("cache_key="):])
		case strings.HasPrefix(mLower, "retry="):
			if n, err := strconv.Atoi(strings.TrimPrefix(mLower, "retry=")); err == nil {
				p.RetryMax = n
			}
		case strings.HasPrefix(mLower, "retry_if="):
			p.RetryPredicate = strings.TrimPrefix(mLower, "retry_if=")
		case strings.HasPrefix(mLower, "backoff="):
			applyBackoffSpec(&p, trimValue(mClean[len("backoff="):]))
		case strings.HasPrefix(mLower, "backoff_base="):
			if d, err := time.ParseDuration(strings.TrimPrefix(mLower, "backoff_base=")); err == nil {
				p.BackoffBase = d
			}
		case strings.HasPrefix(mLower, "backoff_max="):
			if d, err := time.ParseDuration(strings.TrimPrefix(mLower, "backoff_max=")); err == nil {
				p.BackoffMax = d
			}
		case mLower == "idempotent":
			p.Idempotent = true
		case strings.HasPrefix(mLower, "idempotency_header="):
			p.IdempotencyHeader = trimValue(mClean[len("idempotency_header="):])

		case strings.HasPrefix(mLower, "tag="):
			p.Tags = append(p.Tags, splitCSV(mClean[len("tag="):])...)

		case strings.HasPrefix(mLower, "role="):
			p.Roles = append(p.Roles, splitCSV(mClean[len("role="):])...)
		case strings.HasPrefix(mLower, "perm=") || strings.HasPrefix(mLower, "permission="):
			val := strings.TrimPrefix(mClean, "perm=")
			val = strings.TrimPrefix(val, "permission=")
			p.Permissions = append(p.Permissions, splitCSV(val)...)
		case strings.HasPrefix(mLower, "feature="):
			p.Features = append(p.Features, splitCSV(mClean[len("feature="):])...)

		case mLower == "auth":
			p.RequiresAuth = true
		case strings.HasPrefix(mLower, "auth="):
			v := strings.TrimPrefix(mLower, "auth=")
			p.RequiresAuth = v == "required" || v == "true" || v == "yes"

		case strings.HasPrefix(mLower, "scope="):
			p.Scope = strings.TrimPrefix(mLower, "scope=")
		case strings.HasPrefix(mLower, "status="):
			if n, err := strconv.Atoi(strings.TrimPrefix(mLower, "status=")); err == nil {
				p.SuccessStatus = n
			}

		case strings.HasPrefix(mLower, "budget_micros="):
			if n, err := strconv.ParseInt(strings.TrimPrefix(mLower, "budget_micros="), 10, 64); err == nil {
				p.BudgetMicros = n
			}
		case strings.HasPrefix(mLower, "budget="):
			if micros, ok := parseBudget(mClean[len("budget="):]); ok {
				p.BudgetMicros = micros
			}

		case strings.HasPrefix(mLower, "rate_limit="):
			p.RateLimit = trimValue(mClean[len("rate_limit="):])
			p.RateLimitRPS = parseRPS(p.RateLimit)
		case strings.HasPrefix(mLower, "burst="):
			if n, err := strconv.Atoi(strings.TrimPrefix(mLower, "burst=")); err == nil {
				p.RateLimitBurst = n
			}
		case strings.HasPrefix(mLower, "concurrency="):
			if n, err := strconv.ParseInt(strings.TrimPrefix(mLower, "concurrency="), 10, 32); err == nil {
				p.ConcurrencyLimit = int32(n)
			}
		case strings.HasPrefix(mLower, "breaker="):
			applyBreakerSpec(&p, trimValue(mClean[len("breaker="):]))
		case strings.HasPrefix(mLower, "breaker_failures="):
			if n, err := strconv.Atoi(strings.TrimPrefix(mLower, "breaker_failures=")); err == nil {
				p.BreakerFailures = n
			}
		case strings.HasPrefix(mLower, "breaker_cooldown="):
			if d, err := time.ParseDuration(strings.TrimPrefix(mLower, "breaker_cooldown=")); err == nil {
				p.BreakerCooldown = d
			}
		case strings.HasPrefix(mLower, "priority="):
			p.Priority = strings.TrimPrefix(mLower, "priority=")

		case strings.HasPrefix(mLower, "hitl="):
			p.HITLPrompt = trimValue(mClean[len("hitl="):])
		case strings.HasPrefix(mLower, "hitl_options="):
			p.HITLOptions = append(p.HITLOptions, splitCSV(mClean[len("hitl_options="):])...)
		case strings.HasPrefix(mLower, "hitl_trigger=") || strings.HasPrefix(mLower, "hitl_triggers="):
			val := strings.TrimPrefix(mClean, "hitl_trigger=")
			val = strings.TrimPrefix(val, "hitl_triggers=")
			p.HITLTriggers = append(p.HITLTriggers, splitCSV(val)...)

		case mLower == "audit":
			p.Audit = true
		case strings.HasPrefix(mLower, "audit="):
			v := strings.TrimPrefix(mLower, "audit=")
			p.Audit = v == "true" || v == "yes" || v == "1"

		case mLower == "deprecated":
			p.Deprecated = true
		case strings.HasPrefix(mLower, "deprecated="):
			v := strings.TrimPrefix(mLower, "deprecated=")
			p.Deprecated = v == "true" || v == "yes" || v == "1"
		case strings.HasPrefix(mLower, "since="):
			p.DeprecatedSince = trimValue(mClean[len("since="):])
		case strings.HasPrefix(mLower, "use="):
			p.DeprecatedUse = trimValue(mClean[len("use="):])

		case strings.HasPrefix(mLower, "channel="):
			p.SSEChannel = trimValue(mClean[len("channel="):])
		case strings.HasPrefix(mLower, "cli_alias="):
			p.CLIAliases = append(p.CLIAliases, splitCSV(mClean[len("cli_alias="):])...)
		case strings.HasPrefix(mLower, "cli_desc="):
			p.CLIDesc = trimValue(mClean[len("cli_desc="):])
		case strings.HasPrefix(mLower, "a2a_desc="):
			p.A2ADesc = trimValue(mClean[len("a2a_desc="):])
		case strings.HasPrefix(mLower, "a2a_example="):
			p.A2AExample = append(p.A2AExample, splitCSV(mClean[len("a2a_example="):])...)

		default:
			if eq := strings.IndexByte(mClean, '='); eq > 0 {
				key := strings.ToLower(strings.TrimSpace(mClean[:eq]))
				val := trimValue(mClean[eq+1:])
				if b := buildDSLTransportBinding(key, val); b != nil {
					p.Transports = append(p.Transports, *b)
				}
			}
		}
	}
	return p
}

func applyTransportExtras(p *Modifiers) {
	for i := range p.Transports {
		b := &p.Transports[i]
		switch b.Kind {
		case "sse":
			if p.SSEChannel != "" {
				if b.Meta == nil {
					b.Meta = map[string]string{}
				}
				b.Meta["channel"] = p.SSEChannel
			}
		case "cli":
			if len(p.CLIAliases) > 0 {
				b.Aliases = append([]string(nil), p.CLIAliases...)
			}
			if p.CLIDesc != "" {
				if b.Meta == nil {
					b.Meta = map[string]string{}
				}
				b.Meta["description"] = p.CLIDesc
			}
		case "a2a":
			if p.A2ADesc != "" {
				if b.Meta == nil {
					b.Meta = map[string]string{}
				}
				b.Meta["description"] = p.A2ADesc
			}
			if len(p.A2AExample) > 0 {
				if b.Meta == nil {
					b.Meta = map[string]string{}
				}
				b.Meta["examples"] = strings.Join(p.A2AExample, ",")
			}
		}
	}
}

func parseRouteValue(raw string, defaultMethod string) (method, path string) {
	val := trimValue(raw)
	parts := strings.Fields(val)
	switch len(parts) {
	case 2:
		return strings.ToUpper(parts[0]), parts[1]
	case 1:
		return defaultMethod, parts[0]
	default:
		return "", ""
	}
}

func parseBudget(raw string) (int64, bool) {
	raw = trimValue(raw)
	if raw == "" {
		return 0, false
	}
	if strings.HasPrefix(raw, "$") || strings.Contains(raw, ".") {
		f, err := strconv.ParseFloat(strings.TrimPrefix(raw, "$"), 64)
		if err != nil {
			return 0, false
		}
		return int64(f * 1_000_000), true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseRPS(s string) float64 {
	s = strings.TrimSuffix(s, "/s")
	s = strings.TrimSuffix(s, "/sec")
	s = strings.TrimSuffix(s, "rps")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func applyBreakerSpec(p *Modifiers, spec string) {
	if spec == "" {
		return
	}
	if !strings.Contains(spec, "=") {
		if d, err := time.ParseDuration(spec); err == nil {
			p.BreakerCooldown = d
		}
		return
	}
	for _, part := range splitCSV(spec) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		switch k {
		case "failures":
			if n, err := strconv.Atoi(v); err == nil {
				p.BreakerFailures = n
			}
		case "cooldown":
			if d, err := time.ParseDuration(v); err == nil {
				p.BreakerCooldown = d
			}
		}
	}
}

func applyBackoffSpec(p *Modifiers, spec string) {
	if spec == "" {
		return
	}
	parts := splitCSV(spec)
	if len(parts) == 0 {
		return
	}
	p.BackoffStrategy = parts[0]
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		switch k {
		case "base":
			if d, err := time.ParseDuration(v); err == nil {
				p.BackoffBase = d
			}
		case "max":
			if d, err := time.ParseDuration(v); err == nil {
				p.BackoffMax = d
			}
		}
	}
}
