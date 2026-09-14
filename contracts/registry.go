// path: nexssp/flow/contracts/registry.go
package contracts

import "github.com/nexssp/kernel/action"

// Registry resolves action names → capabilities.
type Registry interface {
	Get(name string) (action.AnyAction, bool)
	Actions() []action.AnyAction
}

// PipelineCompiler turns an Arrow DSL expression into a runnable executable.
type PipelineCompiler interface {
	CompilePipeline(expr string) (action.Executable, error)
}
