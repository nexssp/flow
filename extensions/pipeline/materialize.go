package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

// materialize compiles every pipeline declared in meta and mounts it
// on the resolver.
func materialize(req core.MaterializeReq) error {
	pipelines, _ := req.Meta["pipelines"].(map[string]string)
	if len(pipelines) == 0 {
		return nil
	}
	if req.Compile == nil {
		return errors.New("@pipeline: compiler is not available")
	}

	pipelineMods, _ := req.Meta["pipeline_modifiers"].(map[string][]string)

	for name, source := range pipelines {
		mods := pipelineMods[name]

		canonicalName := name
		if !strings.Contains(canonicalName, ".") {
			canonicalName = "pipeline." + canonicalName
		}

		program, err := req.Compile(canonicalName, source, mods...)
		if err != nil {
			return fmt.Errorf("compile sub-pipeline %q: %w", name, err)
		}

		// Rename the action to its canonical name ("pipeline.<name>")
		// so it does not collide with the root atom's name ("runtime.const").
		// Dynamic preserves all modifiers, status codes, tags, and route bindings.
		act := action.Dynamic(program).Name(canonicalName).Build()

		err = req.Resolver.Mount(action.Library{
			Name: "pipeline." + name,
			Actions: []action.AnyAction{
				act,
			},
		})
		if err != nil {
			return fmt.Errorf("mount sub-pipeline %q: %w", name, err)
		}
	}
	return nil
}
