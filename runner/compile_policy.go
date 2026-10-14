package runner

import "github.com/nexssp/flow/compiler"

func compileOptsWithRunnerPolicies(cfg Config) []compiler.CompileOption {
	options := append([]compiler.CompileOption(nil), cfg.CompileOpts...)
	if cfg.Journal == nil && cfg.IdempotencyStore == nil {
		return options
	}
	return append(options, compiler.WithAtomAdvisors(runnerAtomAdvisor(cfg.Journal, cfg.IdempotencyStore)))
}
