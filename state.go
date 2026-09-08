package flow

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/ai/dag"
)

type State struct {
	data map[string]any
}

func NewState(values map[string]any) *State {
	copyValues := make(map[string]any, len(values))
	for k, v := range values {
		copyValues[k] = v
	}
	return &State{data: copyValues}
}

func NewStateFromDAG(dagState *dag.State) *State {
	if dagState == nil {
		return NewState(nil)
	}
	return NewState(dagState.Data())
}

func (s *State) Get(path string) (any, bool) {
	if s == nil || s.data == nil {
		return nil, false
	}

	path = strings.TrimPrefix(path, "state.")

	var current any = s.data
	remaining := path

	for remaining != "" {
		var part string
		dotIdx := strings.IndexByte(remaining, '.')
		if dotIdx == -1 {
			part = remaining
			remaining = ""
		} else {
			part = remaining[:dotIdx]
			remaining = remaining[dotIdx+1:]
		}

		m, ok := current.(map[string]any)
		if !ok {
			val := reflect.ValueOf(current)
			if val.Kind() == reflect.Pointer {
				if val.IsNil() {
					return nil, false
				}
				val = val.Elem()
			}
			if val.Kind() == reflect.Struct {
				field := val.FieldByName(part)
				if !field.IsValid() {
					t := val.Type()
					found := false
					for i := 0; i < t.NumField(); i++ {
						f := t.Field(i)
						tag := f.Tag.Get("json")
						if tag != "" {
							if comma := strings.IndexByte(tag, ','); comma != -1 {
								tag = tag[:comma]
							}
							if strings.EqualFold(tag, part) {
								field = val.Field(i)
								found = true
								break
							}
						}
					}
					if !found {
						return nil, false
					}
				}
				current = field.Interface()
				continue
			}
			return nil, false
		}
		val, exists := m[part]
		if !exists {
			return nil, false
		}
		current = val
	}
	return current, true
}

func EvaluateCondition(condition string, state *State) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, nil
	}

	spaceIdx := strings.IndexAny(condition, " \t\r\n")
	if spaceIdx == -1 {
		return false, fmt.Errorf("graph: invalid condition syntax %q", condition)
	}

	path := condition[:spaceIdx]
	if !strings.HasPrefix(path, "state.") {
		return false, fmt.Errorf("graph: condition path must start with 'state.' prefix: %q", path)
	}

	rest := strings.TrimSpace(condition[spaceIdx:])
	if rest == "exists" {
		_, exists := state.Get(path)
		return exists, nil
	}

	var op string
	var literalStr string
	switch {
	case strings.HasPrefix(rest, "=="):
		op = "=="
		literalStr = strings.TrimSpace(rest[2:])
	case strings.HasPrefix(rest, "!="):
		op = "!="
		literalStr = strings.TrimSpace(rest[2:])
	case strings.HasPrefix(rest, ">="):
		op = ">="
		literalStr = strings.TrimSpace(rest[2:])
	case strings.HasPrefix(rest, "<="):
		op = "<="
		literalStr = strings.TrimSpace(rest[2:])
	case strings.HasPrefix(rest, ">"):
		op = ">"
		literalStr = strings.TrimSpace(rest[1:])
	case strings.HasPrefix(rest, "<"):
		op = "<"
		literalStr = strings.TrimSpace(rest[1:])
	default:
		return false, fmt.Errorf("graph: unsupported operator in condition %q", condition)
	}

	if literalStr == "" {
		return false, fmt.Errorf("graph: missing literal value in condition %q", condition)
	}

	value, exists := state.Get(path)
	if !exists {
		return false, nil
	}

	var want any
	switch {
	case len(literalStr) >= 2 &&
		((literalStr[0] == '"' && literalStr[len(literalStr)-1] == '"') ||
			(literalStr[0] == '\'' && literalStr[len(literalStr)-1] == '\'')):
		want = literalStr[1 : len(literalStr)-1]
	case literalStr == "true":
		want = true
	case literalStr == "false":
		want = false
	default:
		f, err := strconv.ParseFloat(literalStr, 64)
		if err != nil {
			return false, fmt.Errorf("graph: invalid numeric literal %q: %w", literalStr, err)
		}
		want = f
	}

	return compareValues(value, want, op)
}

func compareValues(actual, expected any, operator string) (bool, error) {
	if operator == "==" || operator == "!=" {
		var equal bool

		switch act := actual.(type) {
		case string:
			switch exp := expected.(type) {
			case string:
				equal = act == exp
			case fmt.Stringer:
				equal = act == exp.String()
			}
		case bool:
			if exp, ok := expected.(bool); ok {
				equal = act == exp
			}
		default:
			equal = reflect.DeepEqual(normalizeScalar(actual), normalizeScalar(expected))
		}

		if af, ok := number(actual); ok {
			if ef, ok := number(expected); ok {
				equal = af == ef
			}
		}
		if operator == "!=" {
			return !equal, nil
		}
		return equal, nil
	}

	a, ok := number(actual)
	if !ok {
		return false, fmt.Errorf("graph: relational operator %q requires numeric state value, got %T", operator, actual)
	}
	b, ok := number(expected)
	if !ok {
		return false, fmt.Errorf("graph: relational operator %q requires numeric literal", operator)
	}

	switch operator {
	case ">":
		return a > b, nil
	case ">=":
		return a >= b, nil
	case "<":
		return a < b, nil
	case "<=":
		return a <= b, nil
	default:
		return false, fmt.Errorf("graph: unsupported operator %q", operator)
	}
}

func normalizeScalar(v any) any {
	if s, ok := v.(fmt.Stringer); ok {
		return s.String()
	}
	return v
}

func number(v any) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case float64:
		return val, true
	case int32:
		return float64(val), true
	case float32:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	default:
		return 0, false
	}
}
