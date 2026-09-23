package flow

import (
	"github.com/nexssp/flow/directives/core"
)

// collectAtomAdvisors builds a slice of AtomAdvisor funcs from every
// directive that registered itself as an AtomPolicy. Each advisor
// decides for itself whether it has anything to do with a given atom.
//
// Returns nil when no directive has anything to contribute, so the hot
// path in resolveDynamicNode can skip iteration entirely.
func collectAtomAdvisors(pre *core.Preprocessed) []core.AtomAdvisor {
	policies := core.AtomPolicies()
	if len(policies) == 0 || pre == nil {
		return nil
	}
	out := make([]core.AtomAdvisor, 0, len(policies))
	for _, policy := range policies {
		if advisor := policy.AtomAdvisor(pre); advisor != nil {
			out = append(out, advisor)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// withAtomAdvisors installs a pre-collected slice of advisors.
func withAtomAdvisors(advisors []core.AtomAdvisor) CompileOption {
	return func(c *compileOptions) {
		c.atomAdvisors = advisors
	}
}

// WithAtomAdvisors collects advisors from every registered AtomPolicy
// and attaches them to the compilation. Applications that preprocess a
// file themselves and then call CompilePipeline directly can pass the
// result here.
func WithAtomAdvisors(pre *core.Preprocessed) CompileOption {
	return withAtomAdvisors(collectAtomAdvisors(pre))
}
