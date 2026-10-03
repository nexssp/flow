package pipeline

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/scope"
)

// Directive parses `@pipeline NAME ... @end` and stores the block body in
// meta["pipelines"][NAME]. Inline modifiers after the name — `:key` or
// `:key=value`, with optional single/double-quoted values — are collected
// into meta["pipeline_modifiers"][NAME] and applied to the compiled
// sub-pipeline during materialization.
//
// Each declared pipeline is mounted on the resolver as `pipeline.<name>`
// (the prefix is added only when the name has no dot), so a pipeline may
// reference another by its canonical name.
//
// `@pipeline name` with no modifiers is valid and behaves exactly as it
// did before modifiers were supported.
var Directive = core.Directive{
	Name:    "pipeline",
	Example: "@pipeline pack:route=\"POST /pack\"\n  src -> dst\n@end",
	Handler: handleDirective,
}

// AppliedModifier is a modifier together with the origin that produced
// it. Pipeline directive handlers build these so `nflow explain` can
// attribute every effective modifier to a pipeline or profile.
type AppliedModifier struct {
	Raw    string
	Source core.ModifierSource
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	header := strings.TrimSpace(req.Lines[req.I])

	name, mods := parsePipelineHeader(header)
	if name == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@pipeline requires a name")
	}

	applied, err := expandProfiles(mods, req.Out, pos, name)
	if err != nil {
		return core.DirectiveRes{}, err
	}

	next := req.I + 1
	var bodyLines []string
	for next < len(req.Lines) {
		if strings.TrimSpace(req.Lines[next]) == "@end" {
			next++
			break
		}
		bodyLines = append(bodyLines, req.Lines[next])
		next++
	}

	pipelines, _ := req.Out["pipelines"].(map[string]string)
	if pipelines == nil {
		pipelines = make(map[string]string)
		req.Out["pipelines"] = pipelines
	}
	pipelines[name] = strings.Join(bodyLines, "\n")

	if len(applied) > 0 {
		pipelineMods, _ := req.Out["pipeline_modifiers"].(map[string][]string)
		if pipelineMods == nil {
			pipelineMods = make(map[string][]string)
			req.Out["pipeline_modifiers"] = pipelineMods
		}
		raw := make([]string, len(applied))
		for i, am := range applied {
			raw[i] = am.Raw
		}
		pipelineMods[name] = raw

		sources, _ := req.Out["pipeline_modifier_sources"].(map[string][]core.ModifierSource)
		if sources == nil {
			sources = make(map[string][]core.ModifierSource)
			req.Out["pipeline_modifier_sources"] = sources
		}
		srcs := make([]core.ModifierSource, len(applied))
		for i, am := range applied {
			srcs[i] = am.Source
		}
		sources[name] = srcs
	}

	return core.DirectiveRes{Next: next}, nil
}

// expandProfiles replaces every `:profile=NAME` entry with the profile's
// modifiers, tagging each with its origin. Local modifiers win over
// profile-supplied modifiers on the same name; a profile-supplied name
// wins over an earlier profile-supplied name. The result preserves
// first-seen order.
//
// A reference to an undeclared profile, an unresolved parent, or a
// parent cycle is a compile error, not a silent no-op.
func expandProfiles(mods []string, meta map[string]any, pos core.Position, pipelineName string) ([]AppliedModifier, error) {
	if len(mods) == 0 {
		return nil, nil
	}

	localSource := core.ModifierSource{Kind: "pipeline", Label: pipelineName}

	var expanded []AppliedModifier
	index := make(map[string]int)
	add := func(raw string, src core.ModifierSource) {
		n := core.ModifierName(raw)
		if i, ok := index[n]; ok {
			expanded[i] = AppliedModifier{Raw: raw, Source: src}
			return
		}
		index[n] = len(expanded)
		expanded = append(expanded, AppliedModifier{Raw: raw, Source: src})
	}

	for _, raw := range mods {
		if core.ModifierName(raw) != "profile" {
			add(raw, localSource)
			continue
		}

		value := ""
		if i := strings.IndexByte(raw, '='); i > 0 {
			value = raw[i+1:]
		}
		if value == "" {
			return nil, core.SourceError(pos,
				"@pipeline: :profile requires a name (use :profile=NAME)")
		}

		profileMods, err := scope.ExpandProfile(meta, value)
		if err != nil {
			return nil, core.SourceError(pos, "@pipeline: %v", err)
		}
		profileSource := core.ModifierSource{Kind: "profile", Label: value}
		for _, pm := range profileMods {
			add(pm, profileSource)
		}
	}
	return expanded, nil
}

func parsePipelineHeader(line string) (name string, mods []string) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "@pipeline"))
	if line == "" {
		return "", nil
	}

	nameEnd := strings.IndexAny(line, ": \t")
	if nameEnd < 0 {
		return line, nil
	}

	name = strings.TrimSpace(line[:nameEnd])
	rest := strings.TrimSpace(line[nameEnd:])

	for rest != "" {
		colonIdx := strings.IndexByte(rest, ':')
		if colonIdx < 0 {
			break
		}
		rest = rest[colonIdx+1:]

		key, remaining := readModifierKey(rest)
		rest = remaining
		if key == "" {
			continue
		}

		if rest == "" || rest[0] != '=' {
			mods = append(mods, key)
			rest = strings.TrimSpace(rest)
			continue
		}

		value, remaining := readModifierValue(rest[1:])
		rest = strings.TrimSpace(remaining)
		mods = append(mods, key+"="+value)
	}

	return name, mods
}

func readModifierKey(rest string) (key, remaining string) {
	keyEnd := strings.IndexAny(rest, "=: \t")
	if keyEnd < 0 {
		return strings.TrimSpace(rest), ""
	}
	return strings.TrimSpace(rest[:keyEnd]), rest[keyEnd:]
}

func readModifierValue(rest string) (value, remaining string) {
	if rest == "" {
		return "", ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		return readQuotedValue(rest)
	}
	valueEnd := strings.IndexAny(rest, ": \t")
	if valueEnd < 0 {
		return rest, ""
	}
	return rest[:valueEnd], rest[valueEnd:]
}

func readQuotedValue(rest string) (value, remaining string) {
	quote := rest[0]
	rest = rest[1:]
	var sb strings.Builder
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == '\\' && i+1 < len(rest) {
			sb.WriteByte(rest[i+1])
			i++
			continue
		}
		if c == quote {
			return sb.String(), rest[i+1:]
		}
		sb.WriteByte(c)
	}
	return sb.String(), ""
}
