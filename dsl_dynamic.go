package flow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/transport/tcli"
	"github.com/nexssp/transport/thttp"
	"github.com/nexssp/transportai/ta2a"
	"github.com/nexssp/validation"
)

type DynamicPolicy struct {
	RetryCount  int
	Timeout     time.Duration
	CacheTTL    time.Duration
	Idempotent  bool
	Breaker     bool
	Debug       bool
	Validate    bool
	Coalesce    bool
	Dedup       bool
	HTTPMethod  string
	HTTPPath    string
	StatusCode  int
	CLICommand  string
	CLIDesc     string
	A2ARole     string
	CustomName  string
	Description string
}

var globalCoalescer = action.NewCoalescer()

func resolveDynamicNode(atom *compiler.AtomExpr, reg Registry) (*action.Builder[any, any], error) {
	act, ok := reg.Get(atom.Name)
	if !ok {
		return nil, xerr.NotFound(fmt.Sprintf("flow: capability %q not found in registry", atom.Name))
	}

	dyn := action.Dynamic(act)
	policy := parseModifiers(atom.Modifiers)

	if policy.CustomName != "" {
		dyn.Name(policy.CustomName)
	} else {
		dyn.Name(atom.Name)
	}

	if policy.Description != "" {
		dyn.Description(policy.Description)
	}

	if policy.StatusCode > 0 {
		dyn.SuccessStatus(policy.StatusCode)
	}

	if policy.Timeout > 0 {
		dyn.Timeout(policy.Timeout)
	}

	if policy.RetryCount > 0 {
		dyn.Retry(policy.RetryCount, action.ExponentialJitter(20*time.Millisecond, 500*time.Millisecond))
	}

	if policy.Idempotent {
		dyn.Idempotent()
	}

	// Only add an HTTP route from DSL if the action does NOT already have an existing route in Go
	if policy.HTTPPath != "" {
		existingHasRoute := false

		for _, b := range act.GetBindings() {
			switch b.(type) {
			case thttp.HTTPRoute, thttp.SSERoute, thttp.RawHTTPHandler:
				existingHasRoute = true
			}
		}

		if !existingHasRoute {
			method := strings.ToUpper(policy.HTTPMethod)
			if method == "" {
				method = "POST"
			}

			dyn.Route(thttp.HTTPRoute{Method: method, Path: policy.HTTPPath})
		}
	}

	if policy.CLICommand != "" {
		dyn.Route(tcli.Command(policy.CLICommand, policy.CLIDesc))
	}

	if policy.A2ARole != "" {
		dyn.Route(ta2a.Role(policy.A2ARole))
	}

	hashFn := func(req any) string {
		if req == nil {
			return atom.Name + ":nil"
		}

		b, _ := json.Marshal(req)
		hash := sha256.Sum256(b)

		return atom.Name + ":" + fmt.Sprintf("%x", hash)
	}

	if policy.CacheTTL > 0 {
		dyn.Cache(policy.CacheTTL, hashFn)
	}

	if policy.Coalesce {
		dyn.Coalesce(globalCoalescer, hashFn)
	}

	if policy.Dedup {
		dyn.Dedup(hashFn)
	}

	if policy.Debug {
		dyn.LogCalls(slog.Default())
	}

	if policy.Validate {
		dyn.Validate(func(ctx context.Context, req any) error {
			if req != nil {
				return validation.Struct(ctx, req)
			}

			return nil
		})
	}

	hasInjections := len(atom.Params) > 0 || len(atom.Inputs) > 0 ||
		atom.Prompt != "" || len(atom.Targets) > 0 ||
		len(atom.Excludes) > 0 || atom.Profile != ""

	if hasInjections {
		dyn.HookBefore(func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
			if m, ok := req.(map[string]any); ok {
				for k, v := range atom.Params {
					m[k] = v
				}

				if atom.Prompt != "" {
					m["prompt"] = atom.Prompt
				}

				if len(atom.Targets) > 0 {
					m["targets"] = atom.Targets
				}

				if len(atom.Excludes) > 0 {
					m["excludes"] = atom.Excludes
				}

				if atom.Profile != "" {
					m["profile"] = atom.Profile
				}
			}

			return ctx, nil
		})
	}

	return dyn, nil
}

func parseModifiers(modifiers []string) DynamicPolicy {
	var policy DynamicPolicy

	for _, p := range modifiers {
		p = strings.TrimSpace(p)
		pClean := strings.Trim(p, `"'`)
		pLower := strings.ToLower(pClean)

		switch {
		case strings.HasPrefix(pLower, "name="):
			policy.CustomName = strings.Trim(strings.TrimPrefix(pClean, "name="), `"' `)

		case strings.HasPrefix(pLower, "desc=") || strings.HasPrefix(pLower, "description="):
			val := strings.TrimPrefix(pClean, "desc=")
			val = strings.TrimPrefix(val, "description=")
			policy.Description = strings.Trim(val, `"' `)

		case strings.HasPrefix(pLower, "status="):
			if code, err := strconv.Atoi(strings.TrimPrefix(pLower, "status=")); err == nil {
				policy.StatusCode = code
			}

		case strings.HasPrefix(pLower, "route=") || strings.HasPrefix(pLower, "http="):
			val := strings.TrimPrefix(pClean, "route=")
			val = strings.TrimPrefix(val, "http=")
			val = strings.TrimPrefix(val, "ROUTE=")
			val = strings.TrimPrefix(val, "HTTP=")
			val = strings.Trim(val, `"' `)

			parts := strings.Fields(val)
			if len(parts) == 2 {
				policy.HTTPMethod = parts[0]
				policy.HTTPPath = parts[1]
			} else if len(parts) == 1 {
				policy.HTTPMethod = "POST"
				policy.HTTPPath = parts[0]
			}

		case strings.HasPrefix(pLower, "cli="):
			val := strings.TrimPrefix(pClean, "cli=")
			val = strings.Trim(val, `"' `)
			parts := strings.SplitN(val, ":", 2)

			policy.CLICommand = parts[0]
			if len(parts) > 1 {
				policy.CLIDesc = parts[1]
			}

		case strings.HasPrefix(pLower, "role="):
			policy.A2ARole = strings.Trim(strings.TrimPrefix(pClean, "role="), `"' `)

		case strings.HasPrefix(pLower, "retry="):
			if val, err := strconv.Atoi(strings.TrimPrefix(pLower, "retry=")); err == nil && val > 0 {
				policy.RetryCount = val
			}
		case strings.HasPrefix(pLower, "timeout="):
			if dur, err := time.ParseDuration(strings.TrimPrefix(pLower, "timeout=")); err == nil && dur > 0 {
				policy.Timeout = dur
			}
		case strings.HasPrefix(pLower, "cache="):
			if dur, err := time.ParseDuration(strings.TrimPrefix(pLower, "cache=")); err == nil && dur > 0 {
				policy.CacheTTL = dur
			}
		case pLower == "idempotent":
			policy.Idempotent = true
		case pLower == "breaker":
			policy.Breaker = true
		case pLower == "debug":
			policy.Debug = true
		case pLower == "validate":
			policy.Validate = true
		case pLower == "coalesce":
			policy.Coalesce = true
		case pLower == "dedup":
			policy.Dedup = true
		}
	}

	return policy
}
