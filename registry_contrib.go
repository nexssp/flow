package flow

import (
	"fmt"

	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
)

// ContributeRegistry lets every directive that implements
// core.RegistryContributor add actions the pipeline can call by name.
//
// Called by the runner between preprocessing and pipeline compilation.
// Returns base unchanged when no contributor has anything to add.
func ContributeRegistry(pre *core.Preprocessed, base *action.Registry) (*action.Registry, error) {
	contributors := core.RegistryContributors()
	if len(contributors) == 0 || pre == nil {
		return base, nil
	}

	var contributed []action.AnyAction
	for _, c := range contributors {
		acts, err := c.RegisterActions(pre, base)
		if err != nil {
			return nil, fmt.Errorf("@%s: %w", c.Name(), err)
		}
		contributed = append(contributed, acts...)
	}

	if len(contributed) == 0 {
		return base, nil
	}

	libs := make([]action.Library, 0, 2)
	if base != nil && len(base.Actions()) > 0 {
		libs = append(libs, action.Library{Name: "base", Actions: base.Actions()})
	}
	libs = append(libs, action.Library{Name: "directives", Actions: contributed})

	return action.NewRegistry(libs...)
}
