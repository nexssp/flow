package pipeline

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
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

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	header := strings.TrimSpace(req.Lines[req.I])

	name, mods := parsePipelineHeader(header)
	if name == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@pipeline requires a name")
	}

	expanded, err := expandProfiles(mods, req.Out, pos)
	if err != nil {
		return core.DirectiveRes{}, err
	}
	mods = expanded

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

	if len(mods) > 0 {
		pipelineMods, _ := req.Out["pipeline_modifiers"].(map[string][]string)
		if pipelineMods == nil {
			pipelineMods = make(map[string][]string)
			req.Out["pipeline_modifiers"] = pipelineMods
		}
		pipelineMods[name] = mods
	}

	return core.DirectiveRes{Next: next}, nil
}

// expandProfiles replaces every `:profile=NAME` entry with the profile's
// modifiers. Local modifiers win over profile-supplied modifiers on the
// same name; a profile-supplied name wins over an earlier
// profile-supplied name. The result preserves first-seen order.
//
// A reference to an undeclared profile is a compile error, not a
// silent no-op.
func expandProfiles(mods []string, meta map[string]any, pos core.Position) ([]string, error) {
	if len(mods) == 0 {
		return nil, nil
	}

	profiles, _ := meta["scope.profiles"].(map[string][]string)

	var expanded []string
	index := make(map[string]int)
	add := func(raw string) {
		n := core.ModifierName(raw)
		if i, ok := index[n]; ok {
			expanded[i] = raw
			return
		}
		index[n] = len(expanded)
		expanded = append(expanded, raw)
	}

	for _, raw := range mods {
		name := core.ModifierName(raw)
		if name != "profile" {
			add(raw)
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

		profileMods, ok := profiles[value]
		if !ok {
			return nil, core.SourceError(pos,
				"@pipeline: unknown profile %q (declare with @profile before use)", value)
		}
		for _, pm := range profileMods {
			add(pm)
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
