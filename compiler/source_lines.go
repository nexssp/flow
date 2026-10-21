package compiler

import "strings"

const sourceLinesKey = "compiler.source.lines"

// SourceLine identifies a rendered source line through its embedding ancestry.
// Origins are ordered outermost to innermost. A source-sensitive extension can
// therefore distinguish an included file's lines from the caller's line numbers
// and still apply an enclosing policy at the embedding site.
type SourceLine struct {
	Origins []Position
}

// SourceLinesFromMeta returns immutable-during-use source provenance. Callers
// must not mutate the returned slice or its Origins. No lineage is returned for
// metadata produced outside the standard preprocessing lifecycle.
func SourceLinesFromMeta(meta map[string]any) []SourceLine {
	lines, _ := meta[sourceLinesKey].([]SourceLine)
	return lines
}

// EmbedSource inserts a preprocessed fragment at the current directive's output
// slot and retains its provenance. This is the neutral source-composition path;
// directive handlers still own whether, where and what they embed.
func (req DirectiveReq) EmbedSource(clean string, metadata map[string]any) {
	if req.embedSource != nil {
		req.embedSource(clean, SourceLinesFromMeta(metadata))
		return
	}
	req.Body[req.I] = clean
}

func renderedSourceLines(file string, body []string, embedded map[int][]SourceLine) []SourceLine {
	var result []SourceLine
	for i, text := range body {
		position := Position{File: file, Line: i + 1}
		count := strings.Count(text, "\n") + 1
		children := embedded[i]
		for j := range count {
			origins := []Position{position}
			if len(children) == count {
				origins = append(origins, children[j].Origins...)
			}
			result = append(result, SourceLine{Origins: origins})
		}
	}
	return result
}
