package directives

import "strings"

type atPipeline struct{}

func init() { Register(atPipeline{}) }

func (atPipeline) Name() string { return "pipeline" }

// Syntax:
//
//	@pipeline security_scan
//	  sandbox.sec @{ code: .source_code }
//	  -> { findings: .result, passed: .exit_code == 0 }
//	@end
//
// The @pipeline header and its @end terminator are consumed by this
// directive; the lines in between become Pipeline.Body. The returned
// next index points past @end.
func (atPipeline) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	name, ok := StripDirectivePrefix(line, "pipeline")
	if !ok {
		return 0, AtErr(ctx, i, "pipeline", "malformed directive")
	}
	name = TrimQuotes(name)
	if name == "" {
		return 0, AtErr(ctx, i, "pipeline", "requires a name")
	}

	var body []string

	j := i + 1
	closed := false

	for j < len(lines) {
		if strings.TrimSpace(lines[j]) == "@end" {
			closed = true
			j++
			break
		}
		body = append(body, lines[j])
		j++
	}

	if !closed {
		return 0, AtErrf(ctx, i, "pipeline "+name, "missing @end")
	}

	if _, exists := findPipeline(ctx.Out.Pipelines, name); exists {
		return 0, AtErr(ctx, i, "pipeline "+name, "duplicate declaration")
	}

	ctx.Out.Pipelines = append(ctx.Out.Pipelines, Pipeline{
		Name: name,
		Body: strings.TrimSpace(strings.Join(body, "\n")),
		Pos:  Position{File: ctx.File, Line: i + 1},
	})

	return j, nil
}

// findPipeline searches a pipeline slice by name.
func findPipeline(ps []Pipeline, name string) (Pipeline, bool) {
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}
	return Pipeline{}, false
}
