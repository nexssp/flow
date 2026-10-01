package pipeline

import (
	"errors"
	"fmt"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

// materialize compiles every pipeline declared in meta and mounts it
// on the resolver. Re-declared names are a compile-time error; they
// would otherwise silently shadow each other.
func materialize(req core.MaterializeReq) error {
	pipelines, _ := req.Meta["pipelines"].(map[string]string)
	if len(pipelines) == 0 {
		return nil
	}
	if req.Compile == nil {
		return errors.New("@pipeline: compiler is not available")
	}

	for name, source := range pipelines {
		program, err := req.Compile(name, source)
		if err != nil {
			return fmt.Errorf("compile sub-pipeline %q: %w", name, err)
		}
		err = req.Resolver.Mount(action.Library{
			Name: "pipeline." + name,
			Actions: []action.AnyAction{
				action.Dynamic(program).Name(name).Build(),
			},
		})
		if err != nil {
			return fmt.Errorf("mount sub-pipeline %q: %w", name, err)
		}
	}
	return nil
}
