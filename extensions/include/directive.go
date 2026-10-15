package include

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// Directive parses `@include "path"` and merges the referenced file's
// meta into the parent. Path resolution is relative to the including
// file's directory.
//
// After merging meta, it applies the merged compile-time constants
// (`${ns.key}` references from `@const` and `@const.load`) to the
// parent's own source lines below the @include. Without this step the
// constants live in meta but never reach the parent's text, so a
// `@require { url: "${env.X}" }` after the @include would ship the
// literal `${env.X}` to the bundle resolver.
var Directive = compiler.Directive{
	Name:    "include",
	Example: `@include "shared/child.nflow"`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	target := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "@include")), `"'`)

	if target == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@include requires a file path",
		)
	}

	if !filepath.IsAbs(target) && req.BaseDir != "" {
		target = filepath.Join(req.BaseDir, target)
	}
	absTarget, absErr := filepath.Abs(target)
	if absErr != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@include: %v", absErr,
		)
	}
	target = absTarget

	if req.Recurse == nil {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@include: recurse is not available in this preprocessing context",
		)
	}

	clean, includedMeta, err := req.Recurse(target)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}

	if err := mergeIncludedMeta(req.Out, includedMeta); err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@include: %v", err,
		)
	}

	// Apply the merged constants to the parent's lines below the
	// @include. The included file's own @const / @const.load handlers
	// already substituted its lines; the parent's lines have not been
	// touched, so a `${ns.key}` written after the @include would remain
	// literal text through compile. Applying here closes that gap.
	if consts, ok := req.Out["constants"].(map[string]string); ok {
		compiler.ApplyConstants(req.Lines, req.I+1, consts)
	}

	req.Body[req.I] = strings.Join(strings.Fields(clean), " ")
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

// mergeIncludedMeta folds meta from an included file into the parent
// out map. Pipelines and their modifier/source metadata are merged together by
// pipeline name; constants are merged key-by-key; slice-valued declarations
// (require, llms, pools, ...) are concatenated; everything else is copied only
// when the parent has not set it.
//
// The constants case is not a default-case copy: two files that both
// declare constants must produce the union, and the parent must win
// on key collision. Silent loss on the second @include was the
// original bug.
func mergeIncludedMeta(parent, included map[string]any) error {
	if err := mergeIncludedPipelines(parent, included); err != nil {
		return err
	}

	for key, value := range included {
		switch key {
		case "pipelines", "pipeline_modifiers", "pipeline_modifier_sources":
			// These are merged as one name-keyed record above so that a
			// replacement pipeline cannot retain stale modifier metadata.
			continue

		case "symbols":
			parentSymbols, _ := parent["symbols"].(map[string]compiler.SymbolNamespace)
			incoming, _ := value.(map[string]compiler.SymbolNamespace)
			existing := make(map[string]compiler.SymbolNamespace, len(parentSymbols)+len(incoming))
			maps.Copy(existing, parentSymbols)
			for name, namespace := range incoming {
				if _, dup := existing[name]; dup {
					return fmt.Errorf("duplicate @symbols namespace %q", name)
				}
				existing[name] = namespace
			}
			parent["symbols"] = existing

		case "constants":
			existing, _ := parent["constants"].(map[string]string)
			if existing == nil {
				existing = map[string]string{}
				parent["constants"] = existing
			}
			incoming, _ := value.(map[string]string)
			// Parent wins on collision: an explicit @const in the
			// including file is the more local declaration.
			for k, v := range incoming {
				if _, set := existing[k]; !set {
					existing[k] = v
				}
			}

		case "require", "llms", "sandboxes", "pools", "schemas", "macros":
			parent[key] = concatSlices(parent[key], value)

		default:
			if _, exists := parent[key]; !exists {
				parent[key] = value
			}
		}
	}
	return nil
}

// mergeIncludedPipelines treats each pipeline and its parallel modifier/source
// slices as a single record. Incoming same-name pipelines replace the prior
// declaration; if the replacement has no modifiers, any prior modifier data is
// removed instead of being attached to the new body.
func mergeIncludedPipelines(parent, included map[string]any) error {
	incomingPipelines, _ := included["pipelines"].(map[string]string)
	if len(incomingPipelines) == 0 {
		return nil
	}
	incomingModifiers, _ := included["pipeline_modifiers"].(map[string][]string)
	incomingSources, _ := included["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)

	for name := range incomingPipelines {
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
	if pipelines == nil {
		pipelines = make(map[string]string, len(incomingPipelines))
	}
	maps.Copy(pipelines, incomingPipelines)
	parent["pipelines"] = pipelines

	modifiers, _ := parent["pipeline_modifiers"].(map[string][]string)
	sources, _ := parent["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)
	for name := range incomingPipelines {
		incomingPipelineModifiers, hasModifiers := incomingModifiers[name]
		incomingPipelineSources, hasSources := incomingSources[name]
		if !hasModifiers || !hasSources {
			delete(modifiers, name)
			delete(sources, name)
			continue
		}
		if modifiers == nil {
			modifiers = make(map[string][]string, len(incomingModifiers))
		}
		if sources == nil {
			sources = make(map[string][]compiler.ModifierSource, len(incomingSources))
		}
		modifiers[name] = incomingPipelineModifiers
		sources[name] = incomingPipelineSources
	}
	if len(modifiers) == 0 {
		delete(parent, "pipeline_modifiers")
	} else {
		parent["pipeline_modifiers"] = modifiers
	}
	if len(sources) == 0 {
		delete(parent, "pipeline_modifier_sources")
	} else {
		parent["pipeline_modifier_sources"] = sources
	}
	return nil
}

// concatSlices appends b onto a when both are slices of the same type.
// A non-slice pair returns whichever side is non-nil.
func concatSlices(a, b any) any {
	if a == nil {
		return b
	}
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if av.Kind() != reflect.Slice || bv.Kind() != reflect.Slice {
		return a
	}
	merged := reflect.MakeSlice(av.Type(), 0, av.Len()+bv.Len())
	merged = reflect.AppendSlice(merged, av)
	merged = reflect.AppendSlice(merged, bv)
	return merged.Interface()
}
