package pipeline

import (
	"fmt"
	"maps"

	"github.com/nexssp/flow/compiler"
)

func pipelineMetadata() *compiler.MetadataMerger {
	return &compiler.MetadataMerger{
		Keys:  []string{"pipelines", "pipeline_modifiers", "pipeline_modifier_sources"},
		Merge: mergePipelineMetadata,
	}
}

// mergePipelineMetadata merges pipeline bodies and their parallel modifier
// metadata as one record. All mutable inputs are copied before committing so
// validation failures leave both metadata maps and their nested slices intact.
func mergePipelineMetadata(parent, included map[string]any) error {
	incoming, _ := included["pipelines"].(map[string]string)
	if len(incoming) == 0 {
		return nil
	}
	incomingModifiers, _ := included["pipeline_modifiers"].(map[string][]string)
	incomingSources, _ := included["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)

	for name := range incoming {
		modifiers, hasModifiers := incomingModifiers[name]
		sources, hasSources := incomingSources[name]
		if hasModifiers != hasSources {
			return fmt.Errorf("pipeline %q has incomplete modifier/source metadata", name)
		}
		if hasModifiers && len(modifiers) != len(sources) {
			return fmt.Errorf("pipeline %q has %d modifiers but %d modifier sources", name, len(modifiers), len(sources))
		}
	}

	pipelines, _ := parent["pipelines"].(map[string]string)
	mergedPipelines := make(map[string]string, len(pipelines)+len(incoming))
	maps.Copy(mergedPipelines, pipelines)
	maps.Copy(mergedPipelines, incoming)

	parentModifiers, _ := parent["pipeline_modifiers"].(map[string][]string)
	mergedModifiers := cloneStringSlices(parentModifiers)
	parentSources, _ := parent["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)
	mergedSources := cloneSourceSlices(parentSources)
	for name := range incoming {
		modifiers, hasModifiers := incomingModifiers[name]
		sources, hasSources := incomingSources[name]
		if !hasModifiers || !hasSources {
			delete(mergedModifiers, name)
			delete(mergedSources, name)
			continue
		}
		mergedModifiers[name] = append([]string(nil), modifiers...)
		mergedSources[name] = append([]compiler.ModifierSource(nil), sources...)
	}

	parent["pipelines"] = mergedPipelines
	if len(mergedModifiers) == 0 {
		delete(parent, "pipeline_modifiers")
	} else {
		parent["pipeline_modifiers"] = mergedModifiers
	}
	if len(mergedSources) == 0 {
		delete(parent, "pipeline_modifier_sources")
	} else {
		parent["pipeline_modifier_sources"] = mergedSources
	}
	return nil
}

func cloneStringSlices(source map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(source))
	for key, values := range source {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}

func cloneSourceSlices(source map[string][]compiler.ModifierSource) map[string][]compiler.ModifierSource {
	clone := make(map[string][]compiler.ModifierSource, len(source))
	for key, values := range source {
		clone[key] = append([]compiler.ModifierSource(nil), values...)
	}
	return clone
}
