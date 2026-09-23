package flow

import (
	"fmt"
	"strings"
)

// Profile is a declarative security envelope applied to an entire
// flow at compile time. It is the single place where a user tells the
// runtime "this flow handles untrusted input" or "this flow talks to
// the network" without having to wire guards manually.
//
// Profiles are the mechanism behind the "guards are automatic"
// invariant: the user writes @profile:untrusted_input at the top of
// the file, and every node in the file receives the profile's hooks,
// retry policy, and capability restrictions.
//
// The zero value ("" or "trusted") is the permissive default. Every
// non-default profile is opt-in and documented.
type Profile string

const (
	// ProfileTrusted is the zero-value, permissive default. Use for
	// internal tooling, local scripts, and flows that never touch
	// untrusted input or network.
	ProfileTrusted Profile = "trusted"

	// ProfileUntrustedInput is for flows whose input comes from a
	// user, an HTTP body, a file, or an LLM. The compiler injects
	// prompt-injection filtering and PII redaction hooks on every
	// node that touches LLM traffic.
	ProfileUntrustedInput Profile = "untrusted_input"

	// ProfileNetworkIsolated is for flows that must not reach the
	// public internet. The compiler injects an anti-SSRF hook and,
	// when the flow also declares untrusted input, refuses to compile
	// if any node uses :remote= or :exec=.
	ProfileNetworkIsolated Profile = "network_isolated"
)

// ProfilePolicy is the concrete, machine-readable consequence of a
// Profile. The compiler reads it once, at Compile time, and applies
// every field to every node in the flow.
//
// This struct is deliberately data-only: no functions, no interfaces,
// no side effects. It is JSON-serializable so it can be inspected in
// -vvv trace, unit-tested as a fixture, and eventually loaded from a
// user-supplied YAML.
type ProfilePolicy struct {
	// MaxRetries is the automatic retry budget for transient and
	// validation failures. Zero disables automatic retry; the user
	// can still write :retry=N to override per-node.
	MaxRetries int

	// DefaultHooks lists action names that the compiler resolves
	// from the registry and attaches as Before-hooks on every node
	// in the flow. Missing hooks are a compile-time error: a flow
	// that asks for untrusted_input isolation but does not provide
	// the guard actions cannot be compiled.
	DefaultHooks []string

	// AllowedCapabilities is a list of glob patterns matched against
	// node capabilities. An empty list means "everything allowed",
	// which is the trusted default. A non-empty list means "only
	// matching capabilities may appear in this flow".
	//
	// Patterns are simple globs: "agent.*" matches agent.planner,
	// agent.architect, etc. "*" matches everything.
	AllowedCapabilities []string
}

// DefaultProfiles is the built-in policy table. It is a package-level
// variable, not a constant, so downstream code can extend it with
// custom profiles at init time when a new security envelope is
// genuinely needed. It is not intended to be mutated at runtime.
var DefaultProfiles = map[Profile]ProfilePolicy{
	ProfileTrusted: {
		MaxRetries: 2,
		// Trusted flows get no automatic hooks. If the user wants
		// guards, they write them explicitly.
	},

	ProfileUntrustedInput: {
		MaxRetries: 2,
		DefaultHooks: []string{
			"guard.prompt_injection",
			"guard.pii_redact",
		},
	},

	ProfileNetworkIsolated: {
		MaxRetries: 1,
		DefaultHooks: []string{
			"guard.network_ssrf",
		},
		// Network-isolated flows may only call non-network
		// capabilities. The compiler rejects anything with :remote=,
		// :exec=, or :nats= at compile time. The allowlist below is
		// deliberately narrow; callers who need more must add
		// explicit capabilities to their own profile.
		AllowedCapabilities: []string{
			"agent.*",
			"ai.*",
			"sandbox.*",
			"log.*",
			"bench.*",
		},
	},
}

// LookupProfile resolves a profile name to its policy. An empty name
// resolves to ProfileTrusted. Unknown names are a hard error: a typo
// in @profile: must not silently fall back to the permissive default.
func LookupProfile(name string) (ProfilePolicy, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = string(ProfileTrusted)
	}

	p := Profile(trimmed)

	policy, ok := DefaultProfiles[p]
	if !ok {
		known := make([]string, 0, len(DefaultProfiles))
		for k := range DefaultProfiles {
			known = append(known, string(k))
		}

		return ProfilePolicy{}, fmt.Errorf(
			"flow: unknown profile %q (known: %s)",
			trimmed, strings.Join(known, ", "),
		)
	}

	return policy, nil
}

// Allows reports whether capability is permitted by this policy.
// Empty AllowedCapabilities means "everything allowed". Patterns are
// simple globs matched with a trailing-* shortcut, which covers the
// "agent.*" / "*" cases users actually write.
func (p ProfilePolicy) Allows(capability string) bool {
	if len(p.AllowedCapabilities) == 0 {
		return true
	}

	for _, pattern := range p.AllowedCapabilities {
		if pattern == "*" {
			return true
		}

		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, ".*")
			if capability == prefix || strings.HasPrefix(capability, prefix+".") {
				return true
			}

			continue
		}

		if pattern == capability {
			return true
		}
	}

	return false
}
