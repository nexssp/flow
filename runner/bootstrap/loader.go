package bootstrap

// Loader contributes actions to the running assembly. Loaders run in
// declaration order during Run().
//
// The bootstrap package defines the shape but owns none of the
// concrete loaders. Ecosystem packages (ai/flow/bootstrap,
// ai/llm/bootstrap, jumalu/bootstrap) provide their own. This keeps
// the flow bootstrap free of any direct dependency on ai or jumalu.
//
// A Loader must append to asm.Actions and must not replace the slice:
//
//	func MyLoader(...) bootstrap.Loader {
//	    return func(asm *bootstrap.Assembly) error {
//	        asm.Actions = append(asm.Actions, myActions...)
//	        return nil
//	    }
//	}
type Loader func(*Assembly) error
