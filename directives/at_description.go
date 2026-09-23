package directives

import "strings"

type atDescription struct{}

func init() { Register(atDescription{}) }

func (atDescription) Name() string { return "description" }

// Syntax:
//
//	@description "A searchable example of a Flow pipeline"
//
// Sets both Preprocessed.Description (the file-level metadata used by
// flows.ScanFlowsFolder and inspect tools) and, when an @action
// directive is present, the action's description.
func (atDescription) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	text, ok := StripDirectivePrefix(line, "description")
	if !ok {
		return 0, AtErr(ctx, i, "description", "malformed directive")
	}
	text = TrimQuotes(text)

	if ctx.Out.Description != "" && ctx.Out.Description != text {
		return 0, AtErr(ctx, i, "description",
			"conflict with earlier declaration")
	}
	ctx.Out.Description = text

	if ctx.Out.Action != nil {
		ctx.Out.Action.Description = text
	}

	return i + 1, nil
}
