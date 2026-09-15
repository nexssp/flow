package runner

import (
	"os"

	"github.com/nexssp/ai/agent"
	"github.com/nexssp/ai/agent/eval"
	"github.com/nexssp/ai/agent/sandbox"
	aiflow "github.com/nexssp/ai/flow"
	"github.com/nexssp/flow"
)

// BuildStaticRegistry builds the default library set for `nexss flow`
// and `nexssp flow`. It is not used by applications that own their own
// registry (jumalu, custom products); those call flow.BuildRegistry
// directly with their own libraries.
func BuildStaticRegistry() (*flow.MapRegistry, error) {
	return BuildStaticRegistryWithObserver(nil)
}

func BuildStaticRegistryWithObserver(observer *RunnerObserver) (*flow.MapRegistry, error) {
	provider := ResolveProvider()
	if observer != nil {
		provider = WrapProvider(provider, observer)
	}

	actors := agent.NewActorPool()
	sb := sandbox.NewInProcessEngine(".")

	criticModel := os.Getenv("CRITIC_MODEL")

	critic, err := eval.New(eval.Config{
		Provider: provider,
		Model:    criticModel,
	})
	if err != nil {
		return nil, err
	}

	libs := []flow.Library{
		flow.StandardLibrary(),
		aiflow.Library(provider, sb, actors, critic),
	}

	return flow.BuildRegistry(libs...)
}
