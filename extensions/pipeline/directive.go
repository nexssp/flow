package pipeline

import (
	"context"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/scope"
)

// Directive parses `@pipeline NAME ... @end` and stores the block body in
// meta["pipelines"][NAME]. Inline or indented header modifiers — `:key` or
// `:key=value`, with optional quoted values — are collected
// into meta["pipeline_modifiers"][NAME] and applied to the compiled
// sub-pipeline during materialization.
//
// Each declared pipeline is mounted on the resolver as `pipeline.<name>`
// (the prefix is added only when the name has no dot), so a pipeline may
// reference another by its canonical name.
//
// `@pipeline name` with no modifiers is valid and behaves exactly as it
// did before modifiers were supported.
var Directive = compiler.Directive{
	Name:    ID,
	Example: "@pipeline pack:route=\"POST /pack\"\n  src -> dst\n@end",
	Handler: handleDirective,
}

// AppliedModifier is a modifier together with the origin that produced
// it. Pipeline directive handlers build these so `nflow explain` can
// attribute every effective modifier to a pipeline or profile.
type AppliedModifier struct {
	Raw    string
	Source compiler.ModifierSource
	Quoted bool
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	header, err := compiler.ParseDeclarationHeaderWithSpans(req, ID)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	name, mods, quoted, next := header.Name, header.Modifiers, header.QuotedModifiers, header.Next
	keywordSpan, nameSpan, headerSpan := header.KeywordSpan, header.NameSpan, header.HeaderSpan

	canonical := name
	if !strings.Contains(canonical, ".") {
		canonical = "pipeline." + canonical
	}

	var bodyLines []string
	bodyStart := next
	endLine := -1
	for next < len(req.Lines) {
		if strings.TrimSpace(req.Lines[next]) == "@end" {
			endLine = next
			next++
			break
		}
		bodyLines = append(bodyLines, req.Lines[next])
		next++
	}
	blockEnd := req.Offset(len(req.Lines), 0)
	bodyEnd := blockEnd
	if endLine >= 0 {
		terminator := req.LineSpan(endLine)
		blockEnd = terminator.End
		bodyEnd = terminator.Start
	}
	req.RecordBlock(compiler.SourceBlock{
		Kind:       ID,
		Span:       compiler.SourceSpan{Start: keywordSpan.Start, End: blockEnd},
		HeaderSpan: headerSpan,
		BodySpan:   compiler.SourceSpan{Start: req.Offset(bodyStart, 0), End: bodyEnd},
	})
	req.RecordDeclaration(compiler.SourceDeclaration{
		Kind: ID, Name: canonical, KeywordSpan: keywordSpan, NameSpan: nameSpan, Span: headerSpan,
	})
	if declareErr := req.DeclareAt(ID, canonical, keywordSpan, nameSpan, headerSpan); declareErr != nil {
		return compiler.DirectiveRes{Next: next}, declareErr
	}

	applied, err := expandProfiles(mods, quoted, req.Out, pos, name)
	if err != nil {
		return compiler.DirectiveRes{Next: next}, err
	}

	pipelines, _ := req.Out["pipelines"].(map[string]string)
	if pipelines == nil {
		pipelines = make(map[string]string)
		req.Out["pipelines"] = pipelines
	}
	pipelines[name] = strings.Join(bodyLines, "\n")
	positions, _ := req.Out["pipeline_declaration_positions"].(map[string]compiler.Position)
	if positions == nil {
		positions = make(map[string]compiler.Position)
	}
	positions[name] = compiler.Position{File: req.File, Line: req.I + 1, Col: 1}
	req.Out["pipeline_declaration_positions"] = positions

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

		pipelineQuoted, _ := req.Out["pipeline_modifier_quoted"].(map[string][]bool)
		if pipelineQuoted == nil {
			pipelineQuoted = make(map[string][]bool)
			req.Out["pipeline_modifier_quoted"] = pipelineQuoted
		}
		quotedValues := make([]bool, len(applied))
		for i, am := range applied {
			quotedValues[i] = am.Quoted
		}
		pipelineQuoted[name] = quotedValues

		sources, _ := req.Out["pipeline_modifier_sources"].(map[string][]compiler.ModifierSource)
		if sources == nil {
			sources = make(map[string][]compiler.ModifierSource)
			req.Out["pipeline_modifier_sources"] = sources
		}
		srcs := make([]compiler.ModifierSource, len(applied))
		for i, am := range applied {
			srcs[i] = am.Source
		}
		sources[name] = srcs
	}

	return compiler.DirectiveRes{Next: next}, nil
}

// expandProfiles replaces every `:profile=NAME` entry with the profile's
// modifiers, tagging each with its origin. Local modifiers win over
// profile-supplied modifiers on the same name; a profile-supplied name
// wins over an earlier profile-supplied name. The result preserves
// first-seen order.
//
// A reference to an undeclared profile, an unresolved parent, or a
// parent cycle is a compile error, not a silent no-op.
func expandProfiles(mods []string, quoted []bool, meta map[string]any, pos compiler.Position, pipelineName string) ([]AppliedModifier, error) {
	if len(mods) == 0 {
		return nil, nil
	}

	localSource := compiler.ModifierSource{Kind: ID, Label: pipelineName}

	var expanded []AppliedModifier
	index := make(map[string]int)
	add := func(raw string, src compiler.ModifierSource, isQuoted bool) {
		n := compiler.ModifierName(raw)
		if i, ok := index[n]; ok {
			expanded[i] = AppliedModifier{Raw: raw, Source: src, Quoted: isQuoted}
			return
		}
		index[n] = len(expanded)
		expanded = append(expanded, AppliedModifier{Raw: raw, Source: src, Quoted: isQuoted})
	}

	for i, raw := range mods {
		if compiler.ModifierName(raw) != "profile" {
			add(raw, localSource, quotedAt(quoted, i))
			continue
		}

		value := ""
		if i := strings.IndexByte(raw, '='); i > 0 {
			value = raw[i+1:]
		}
		if value == "" {
			return nil, compiler.SourceError(pos,
				"@pipeline: :profile requires a name (use :profile=NAME)")
		}

		profileMods, profileQuoted, err := scope.ExpandProfileWithQuotes(meta, value)
		if err != nil {
			return nil, compiler.SourceError(pos, "@pipeline: %v", err)
		}
		profileSource := compiler.ModifierSource{Kind: "profile", Label: value}
		for i, pm := range profileMods {
			add(pm, profileSource, quotedAt(profileQuoted, i))
		}
	}
	return expanded, nil
}

func quotedAt(quoted []bool, index int) bool {
	return index >= 0 && index < len(quoted) && quoted[index]
}

// EndDirective rejects a stray `@end`. It only fires when `@end` appears
// outside a `@pipeline` block, because the pipeline handler consumes its
// own terminator while scanning the body and never forwards it to the
// directive table. Outside a pipeline the fallback path is the macro
// primary, which reports `unknown macro @end` — a diagnostic that hides
// the actual problem. This handler replaces it with a message that
// names the missing opening directive.
//
// Registered in Bundle().Directives so every top-level `@end` in any
// fixture is a registered directive; the native fixture contract test
// enforces that invariant across every embedded .nflow file.
var EndDirective = compiler.Directive{
	Name:    "end",
	Example: "@pipeline p\n  ...\n@end",
	Handler: handleEnd,
}

func handleEnd(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	return compiler.DirectiveRes{}, compiler.SourceError(
		compiler.Position{File: req.File, Line: req.I + 1},
		"@end without a matching block directive")
}
