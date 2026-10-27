package data

import (
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type whereConfig struct {
	Expression string `json:"expression" validate:"required"`
}

type selectConfig struct {
	Fields string `json:"fields" validate:"required"`
}

// WhereOperator filters JSON object rows using an expr-lang boolean expression.
func WhereOperator() action.NamedOperator {
	return action.NewOperator("data.where", Where)
}

// SelectOperator keeps the requested top-level fields on each row. A requested
// but absent field is retained with a nil value to keep the output shape stable.
func SelectOperator() action.NamedOperator {
	return action.NewOperator("data.select", Select)
}

// Where builds a stream operator; expressions see row fields plus `row` and
// `item`, which both refer to the complete object.
func Where(cfg whereConfig) action.StreamOp[any, any] {
	expression := strings.TrimSpace(cfg.Expression)
	if expression == "" {
		return streamError(xerr.Validation("data.where: expression is required"))
	}
	program, err := expr.Compile(expression, expr.AllowUndefinedVariables())
	if err != nil {
		return streamError(xerr.Validation("data.where: invalid expression: " + err.Error()))
	}
	return func(upstream iter.Seq2[any, error]) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			for value, upstreamErr := range upstream {
				if upstreamErr != nil {
					if !yield(nil, upstreamErr) {
						return
					}
					continue
				}
				row, ok := value.(map[string]any)
				if !ok {
					if !yield(nil, xerr.BadRequest("data.where: input row is not a JSON object")) {
						return
					}
					continue
				}
				result, runErr := expr.Run(program, expressionEnv(row))
				if runErr != nil {
					if !yield(nil, xerr.BadRequest("data.where: expression failed: "+runErr.Error())) {
						return
					}
					continue
				}
				keep, ok := result.(bool)
				if !ok {
					if !yield(nil, xerr.BadRequest("data.where: expression must return a boolean")) {
						return
					}
					continue
				}
				if keep && !yield(row, nil) {
					return
				}
			}
		}
	}
}

// Select applies a top-level field projection to each object row.
func Select(cfg selectConfig) action.StreamOp[any, any] {
	fields := parseFields(cfg.Fields)
	if len(fields) == 0 {
		return streamError(xerr.Validation("data.select: fields is required"))
	}
	if len(fields) == 1 && fields[0] == "*" {
		return func(upstream iter.Seq2[any, error]) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				for value, upstreamErr := range upstream {
					if upstreamErr != nil {
						if !yield(nil, upstreamErr) {
							return
						}
						continue
					}
					row, ok := value.(map[string]any)
					if !ok {
						if !yield(nil, xerr.BadRequest("data.select: input row is not a JSON object")) {
							return
						}
						continue
					}
					if !yield(maps.Clone(row), nil) {
						return
					}
				}
			}
		}
	}
	if slices.Contains(fields, "*") {
		return streamError(xerr.Validation("data.select: * cannot be combined with named fields"))
	}
	return func(upstream iter.Seq2[any, error]) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			for value, upstreamErr := range upstream {
				if upstreamErr != nil {
					if !yield(nil, upstreamErr) {
						return
					}
					continue
				}
				row, ok := value.(map[string]any)
				if !ok {
					if !yield(nil, xerr.BadRequest("data.select: input row is not a JSON object")) {
						return
					}
					continue
				}
				selected := make(map[string]any, len(fields))
				for _, field := range fields {
					selected[field] = row[field]
				}
				if !yield(selected, nil) {
					return
				}
			}
		}
	}
}

func parseFields(raw string) []string {
	parts := strings.Split(raw, ",")
	fields := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		if _, duplicate := seen[field]; duplicate {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	return fields
}

func expressionEnv(row map[string]any) map[string]any {
	env := maps.Clone(row)
	env["row"] = row
	env["item"] = row
	return env
}

func streamError(err error) action.StreamOp[any, any] {
	return func(iter.Seq2[any, error]) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			yield(nil, err)
		}
	}
}
