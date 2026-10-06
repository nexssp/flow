package include

import (
	"context"
	"maps"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/nexssp/flow/core"
)

// Directive parses `@include "path"` and merges the referenced file's
// meta into the parent. Path resolution is relative to the including
// file's directory.
var Directive = core.Directive{
	Name:    "include",
	Example: `@include "shared/child.nflow"`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	target := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "@include")), `"'`)

	if target == "" {
		return core.DirectiveRes{}, core.SourceError(
			core.Position{File: req.File, Line: req.I + 1},
			"@include requires a file path",
		)
	}

	if !filepath.IsAbs(target) && req.BaseDir != "" {
		target = filepath.Join(req.BaseDir, target)
	}
	absTarget, absErr := filepath.Abs(target)
	if absErr != nil {
		return core.DirectiveRes{}, core.SourceError(
			core.Position{File: req.File, Line: req.I + 1},
			"@include: %v", absErr,
		)
	}
	target = absTarget

	if req.Recurse == nil {
		return core.DirectiveRes{}, core.SourceError(
			core.Position{File: req.File, Line: req.I + 1},
			"@include: recurse is not available in this preprocessing context",
		)
	}

	clean, includedMeta, err := req.Recurse(target)
	if err != nil {
		return core.DirectiveRes{}, err
	}

	mergeIncludedMeta(req.Out, includedMeta)

	req.Body[req.I] = strings.Join(strings.Fields(clean), " ")
	return core.DirectiveRes{Next: req.I + 1}, nil
}

// mergeIncludedMeta folds meta from an included file into the parent
// out map. Pipelines are merged key-by-key; slice-valued declarations
// (require, llms, pools, ...) are concatenated; everything else is
// copied only when the parent has not set it.
func mergeIncludedMeta(parent, included map[string]any) {
	for key, value := range included {
		switch key {
		case "pipelines":
			existing, _ := parent["pipelines"].(map[string]string)
			if existing == nil {
				existing = map[string]string{}
			}
			incoming, _ := value.(map[string]string)
			maps.Copy(existing, incoming)
			parent["pipelines"] = existing
		case "require", "llms", "sandboxes", "pools", "schemas", "macros":
			parent[key] = concatSlices(parent[key], value)
		default:
			if _, exists := parent[key]; !exists {
				parent[key] = value
			}
		}
	}
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
