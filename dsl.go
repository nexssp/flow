package flow

import (
	"context"
	"strings"

	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

func CompilePipeline(expr string, reg Registry) (*action.Builder[any, any], error) {
	expr = stripOuterParens(strings.TrimSpace(expr))
	if expr == "" {
		return nil, xerr.BadRequest("flow: pipeline expression cannot be empty")
	}

	if strings.HasPrefix(expr, "loop(") && strings.Contains(expr, ") until(") {
		bodyExpr, untilCond, err := parseLoopExpr(expr)
		if err != nil {
			return nil, err
		}

		bodyBld, err := CompilePipeline(bodyExpr, reg)
		if err != nil {
			return nil, err
		}

		loopAct, err := nodes.NewLoopAction(bodyBld.Build(), untilCond, 15)
		if err != nil {
			return nil, err
		}
		return action.Dynamic(loopAct), nil
	}

	if isSingleBracedBlock(expr) {
		projAct, err := nodes.NewProjectionAction(expr)
		if err != nil {
			return nil, err
		}
		return action.Dynamic(projAct), nil
	}

	if parts := splitTopLevel(expr, '?'); len(parts) == 2 {
		gateBld, err := CompilePipeline(parts[0], reg)
		if err != nil {
			return nil, err
		}
		targetBld, err := CompilePipeline(parts[1], reg)
		if err != nil {
			return nil, err
		}

		routes := map[string]action.AnyAction{
			"proceed": targetBld.Build(),
			"skip": action.New("skip", func(_ context.Context, input any) (any, error) {
				return input, nil
			}).Build(),
		}

		branch := action.BranchAny("conditional_gate", routes, func(_ context.Context, input any) (string, error) {
			if isTruthy(input) {
				return "proceed", nil
			}
			return "skip", nil
		})

		return action.Pipe[any, any, any]("gate_pipe", gateBld.Build(), branch.Build()), nil
	}

	if parts := splitTopLevelAll(expr, "||"); len(parts) > 1 {
		builders := make([]*action.Builder[any, any], len(parts))
		for i, p := range parts {
			bld, err := CompilePipeline(p, reg)
			if err != nil {
				return nil, err
			}
			builders[i] = bld
		}
		return action.FirstSuccess("fallback_chain", builders...), nil
	}

	if parts := splitTopLevelAll(expr, "->"); len(parts) > 1 {
		return compileSequentialPipe(parts, reg)
	}
	if parts := splitTopLevelPipe(expr); len(parts) > 1 {
		return compileSequentialPipe(parts, reg)
	}

	if strings.Contains(expr, "&") {
		parts := splitTopLevel(expr, '&')
		if len(parts) > 1 {
			routes := make(map[string]action.AnyAction, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				bld, err := CompilePipeline(p, reg)
				if err != nil {
					return nil, err
				}
				routes[bld.Describe().Name] = bld.Build()
			}
			parallel := action.ParallelNamed[any]("parallel_group", routes)
			return action.New("parallel_wrap", func(ctx context.Context, req any) (any, error) {
				return parallel.Build().Do(ctx, req)
			}), nil
		}
	}

	return resolveActionNode(expr, reg)
}

func compileSequentialPipe(parts []string, reg Registry) (*action.Builder[any, any], error) {
	if len(parts) == 1 {
		return CompilePipeline(parts[0], reg)
	}

	leftBld, err := CompilePipeline(parts[0], reg)
	if err != nil {
		return nil, err
	}

	rightBld, err := compileSequentialPipe(parts[1:], reg)
	if err != nil {
		return nil, err
	}

	return action.Pipe[any, any, any]("pipe_step", leftBld.Build(), rightBld.Build()), nil
}

func resolveActionNode(expr string, reg Registry) (*action.Builder[any, any], error) {
	return resolveDynamicNode(expr, reg)
}

func parseTokenParams(token string) (string, map[string]any) {
	params := make(map[string]any)

	if idx := strings.Index(token, "@"); idx != -1 {
		params["prompt"] = strings.TrimSpace(token[idx+1:])
		token = token[:idx]
	}
	if idx := strings.Index(token, "~"); idx != -1 {
		exPart := token[idx+1:]
		token = token[:idx]
		params["excludes"] = strings.Split(exPart, ",")
	}
	if idx := strings.Index(token, "#"); idx != -1 {
		params["targets"] = []string{strings.TrimSpace(token[idx+1:])}
		token = token[:idx]
	}
	if idx := strings.Index(token, ":"); idx != -1 {
		params["profile"] = strings.TrimSpace(token[idx+1:])
		token = token[:idx]
	}

	return strings.TrimSpace(token), params
}

func isSingleBracedBlock(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return false
	}

	depth := 0
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i == len(s)-1
			}
		}
	}
	return false
}

func stripOuterParens(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		depth := 0
		matched := false
		for i, r := range s {
			if r == '(' {
				depth++
			} else if r == ')' {
				depth--
				if depth == 0 {
					matched = (i == len(s)-1)
					break
				}
			}
		}
		if matched {
			s = strings.TrimSpace(s[1 : len(s)-1])
		} else {
			break
		}
	}
	return s
}

func splitTopLevel(s string, delimiter rune) []string {
	var parts []string
	depth := 0
	current := strings.Builder{}

	for _, r := range s {
		switch r {
		case '(', '{', '[':
			depth++
			current.WriteRune(r)
		case ')', '}', ']':
			depth--
			current.WriteRune(r)
		default:
			if r == delimiter && depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func splitTopLevelPipe(s string) []string {
	var parts []string
	depth := 0
	start := 0

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 {
				isDouble := (i+1 < len(s) && s[i+1] == '|') || (i > 0 && s[i-1] == '|')
				if !isDouble {
					parts = append(parts, s[start:i])
					start = i + 1
				}
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func splitTopLevelAll(s, delimiter string) []string {
	var parts []string
	parens, braces, brackets := 0, 0, 0
	dLen := len(delimiter)
	start := 0

	for i := 0; i < len(s); {
		switch s[i] {
		case '(':
			parens++
		case ')':
			if parens > 0 {
				parens--
			}
		case '{':
			braces++
		case '}':
			if braces > 0 {
				braces--
			}
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			}
		}

		if parens == 0 && braces == 0 && brackets == 0 && i+dLen <= len(s) && s[i:i+dLen] == delimiter {
			parts = append(parts, s[start:i])
			i += dLen
			start = i
		} else {
			i++
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case int, int64:
		return val != 0
	case string:
		lower := strings.ToLower(val)
		return lower == "true" || lower == "ok" || lower == "approved"
	case map[string]any:
		if approved, ok := val["approved"].(bool); ok {
			return approved
		}
	}
	return true
}

func parseLoopExpr(expr string) (string, string, error) {
	startBody := strings.Index(expr, "loop(") + 5
	untilIdx := strings.Index(expr, ") until(")
	if startBody < 0 || untilIdx == -1 {
		return "", "", xerr.BadRequest("flow: malformed loop expression")
	}

	body := expr[startBody:untilIdx]
	rest := expr[untilIdx+8:]
	endUntil := strings.LastIndex(rest, ")")
	if endUntil == -1 {
		return "", "", xerr.BadRequest("flow: missing closing parenthesis in loop until condition")
	}

	untilCond := rest[:endUntil]
	return strings.TrimSpace(body), strings.TrimSpace(untilCond), nil
}
