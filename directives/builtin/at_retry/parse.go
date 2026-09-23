package at_retry

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/flow/directives/core"
)

func splitField(line string) (key, value string, ok bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

func applyField(decl *Decl, key, value string, ctx *core.Context, bodyLine int, name string) error {
	switch key {
	case "attempts":
		n, err := strconv.Atoi(strings.Trim(value, `"'`))
		if err != nil {
			return core.AtErrf(ctx, bodyLine, "retry "+name, "attempts: %v", err)
		}
		if n <= 0 {
			return core.AtErrf(ctx, bodyLine, "retry "+name,
				"attempts must be > 0, got %d", n)
		}
		decl.MaxAttempts = n

	case "backoff":
		kind := strings.Trim(strings.ToLower(value), `"'`)
		switch kind {
		case "exponential", "linear", "constant":
			decl.BackoffKind = kind
		default:
			return core.AtErrf(ctx, bodyLine, "retry "+name,
				"backoff: unknown kind %q (exponential|linear|constant)", kind)
		}

	case "base":
		d, err := time.ParseDuration(strings.Trim(value, `"'`))
		if err != nil {
			return core.AtErrf(ctx, bodyLine, "retry "+name, "base: %v", err)
		}
		if d <= 0 {
			return core.AtErrf(ctx, bodyLine, "retry "+name, "base must be > 0")
		}
		decl.BackoffBase = d

	case "max":
		d, err := time.ParseDuration(strings.Trim(value, `"'`))
		if err != nil {
			return core.AtErrf(ctx, bodyLine, "retry "+name, "max: %v", err)
		}
		if d <= 0 {
			return core.AtErrf(ctx, bodyLine, "retry "+name, "max must be > 0")
		}
		decl.BackoffMax = d

	case "jitter":
		v := strings.Trim(strings.ToLower(value), `"'`)
		switch v {
		case "true", "yes", "1", "on":
			decl.Jitter = true
		case "false", "no", "0", "off":
			decl.Jitter = false
		default:
			return core.AtErrf(ctx, bodyLine, "retry "+name,
				"jitter: expected true|false, got %q", value)
		}

	case "only":
		kinds, err := parseOnlyList(value)
		if err != nil {
			return core.AtErrf(ctx, bodyLine, "retry "+name, "only: %v", err)
		}
		decl.Only = kinds

	default:
		return core.AtErrf(ctx, bodyLine, "retry "+name,
			"unknown field %q (attempts|backoff|base|max|jitter|only)", key)
	}

	return nil
}

func parseOnlyList(value string) ([]string, error) {
	v := strings.TrimSpace(value)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")

	if strings.TrimSpace(v) == "" {
		return nil, nil
	}

	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p == "" {
			continue
		}
		if !core.IsKnownXerrKind(p) {
			return nil, fmt.Errorf(
				"unknown xerr kind %q (known: %s)",
				p, strings.Join(core.KnownXerrKinds(), ", "))
		}
		out = append(out, p)
	}
	return out, nil
}
