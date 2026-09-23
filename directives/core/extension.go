package core

import "github.com/nexssp/kernel/action"

// The three extension points a directive may opt into. A directive
// implements none, some, or all. The compiler discovers the ones
// implemented by iterating the registry once, at startup.

// RegistryContributor lets a directive register actions the pipeline
// can call by name. @route, @pipeline, and @pool use this. Called
// once, before compilation.
type RegistryContributor interface {
	Directive
	RegisterActions(pre *Preprocessed, base *action.Registry) ([]action.AnyAction, error)
}

// PipelineWrapper lets a directive wrap the whole compiled pipeline
// with additional behaviour. @on_error uses this. Called once, after
// compilation.
type PipelineWrapper interface {
	Directive
	WrapPipeline(pre *Preprocessed, inner action.Executable) (action.Executable, error)
}

// AtomAdvisor is called once per atom during the AST walk. It inspects
// the atom name and modifiers, and may attach middleware to the atom's
// builder. Returning without doing anything is the fast path.
//
// The advisor must not retain the builder or the modifiers slice beyond
// the call.
type AtomAdvisor func(atomName string, builder *action.Builder[any, any], modifiers []string)

// AtomPolicy lets a directive influence individual atoms. @retry (and
// future @fallback, @approval) use this. Called once per atom, only
// when a matching declaration exists.
type AtomPolicy interface {
	Directive
	AtomAdvisor(pre *Preprocessed) AtomAdvisor
}

var (
	registryContributors []RegistryContributor
	pipelineWrappers     []PipelineWrapper
	atomPolicies         []AtomPolicy
)

// RegisterRegistryContributor opts a directive into the pre-compile
// action registration phase.
func RegisterRegistryContributor(rc RegistryContributor) {
	if rc == nil {
		panic("directives/core: RegisterRegistryContributor(nil)")
	}
	registryContributors = append(registryContributors, rc)
}

// RegisterPipelineWrapper opts a directive into the post-compile
// pipeline wrapping phase.
func RegisterPipelineWrapper(pw PipelineWrapper) {
	if pw == nil {
		panic("directives/core: RegisterPipelineWrapper(nil)")
	}
	pipelineWrappers = append(pipelineWrappers, pw)
}

// RegisterAtomPolicy opts a directive into the per-atom advice phase.
func RegisterAtomPolicy(ap AtomPolicy) {
	if ap == nil {
		panic("directives/core: RegisterAtomPolicy(nil)")
	}
	atomPolicies = append(atomPolicies, ap)
}

// RegistryContributors returns a copy of the registered contributors.
func RegistryContributors() []RegistryContributor {
	out := make([]RegistryContributor, len(registryContributors))
	copy(out, registryContributors)
	return out
}

// PipelineWrappers returns a copy of the registered wrappers.
func PipelineWrappers() []PipelineWrapper {
	out := make([]PipelineWrapper, len(pipelineWrappers))
	copy(out, pipelineWrappers)
	return out
}

// AtomPolicies returns a copy of the registered atom policies.
func AtomPolicies() []AtomPolicy {
	out := make([]AtomPolicy, len(atomPolicies))
	copy(out, atomPolicies)
	return out
}
