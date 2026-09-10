package nodes

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/action"
)

var projectionCounter atomic.Int64

// NewProjectionAction compiles inline data shaping { key: expr } using precompiled expr bytecode.
// Supports standard expressions, JQ-style field prefixes ({ to: .user }), and root access ({ out: . }).
func NewProjectionAction(code string) (*action.BuiltAction[any, any], error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, "{") || !strings.HasSuffix(code, "}") {
		code = "{" + code + "}"
	}

	compiledCode, usesRoot := preprocessProjectionCode(code)

	program, err := expr.Compile(compiledCode, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("flow: invalid projection expression %q: %w", code, err)
	}

	nodeID := fmt.Sprintf("projection_%d", projectionCounter.Add(1))

	return action.New(nodeID, func(ctx context.Context, input any) (any, error) {
		env := normalizeProjectionEnv(input)

		if usesRoot {
			if m, ok := env.(map[string]any); ok {
				cloned := make(map[string]any, len(m)+1)
				for k, v := range m {
					cloned[k] = v
				}

				cloned["__root__"] = input
				env = cloned
			} else {
				env = map[string]any{
					"__root__": input,
				}
			}
		}

		out, runErr := expr.Run(program, env)
		if runErr != nil {
			return nil, fmt.Errorf("flow: projection failed: %w", runErr)
		}

		return out, nil
	}).
		Internal().
		Build(), nil
}

func preprocessProjectionCode(code string) (string, bool) {
	usesRoot := false

	var sb strings.Builder
	sb.Grow(len(code) + 16)

	var inQuote byte

	n := len(code)

	for i := 0; i < n; i++ {
		ch := code[i]

		// Track string literal boundaries (do not mutate dots inside quotes)
		if inQuote != 0 {
			if ch == inQuote && (i == 0 || code[i-1] != '\\') {
				inQuote = 0
			}

			sb.WriteByte(ch)

			continue
		}

		if ch == '"' || ch == '\'' {
			inQuote = ch
			sb.WriteByte(ch)

			continue
		}

		if ch == '.' {
			// Do not touch floating-point numbers like 3.14
			if i > 0 && code[i-1] >= '0' && code[i-1] <= '9' {
				sb.WriteByte(ch)

				continue
			}

			// Do not touch chained member access like issue.title or user.Address.City
			if i > 0 && (isIdentRune(rune(code[i-1])) || code[i-1] == ')' || code[i-1] == ']') {
				sb.WriteByte(ch)

				continue
			}

			// Leading dot before identifier (.field) -> strip leading dot
			if i+1 < n && isIdentStart(rune(code[i+1])) {
				continue
			}

			// Standalone root dot (.) -> __root__
			sb.WriteString("__root__")

			usesRoot = true

			continue
		}

		sb.WriteByte(ch)
	}

	return sb.String(), usesRoot
}

func isIdentStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isIdentRune(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9')
}

func normalizeProjectionEnv(input any) any {
	if input == nil {
		return nil
	}

	// 1. Obsługa map (np. wyniki z gałęzi równoległych)
	if m, ok := input.(map[string]any); ok {
		out := make(map[string]any, len(m)*3)
		for k, v := range m {
			// Rekursywnie normalizuj zagnieżdżone wartości (np. struktury wewnątrz mapy!)
			normalizedVal := normalizeProjectionEnv(v)

			out[k] = normalizedVal
			out[strings.ToLower(k)] = normalizedVal

			// Jeśli klucz zawiera kropkę, np. "agent.sandbox", rozwiń go do zagnieżdżonej mapy
			if strings.ContainsRune(k, '.') {
				parts := strings.Split(k, ".")

				curr := out
				for i := 0; i < len(parts)-1; i++ {
					sub, exists := curr[parts[i]]
					if !exists {
						subMap := make(map[string]any)
						curr[parts[i]] = subMap
						curr = subMap
					} else if subMap, ok := sub.(map[string]any); ok {
						curr = subMap
					}
				}

				curr[parts[len(parts)-1]] = normalizedVal
			}
		}

		return out
	}

	// 2. Obsługa struktur Go przez refleksję
	rv := reflect.ValueOf(input)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}

		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return input
	}

	typ := rv.Type()
	out := make(map[string]any, typ.NumField()*3)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		val := rv.Field(i).Interface()
		// Rekursywna normalizacja zagnieżdżonych struktur
		normalizedVal := normalizeProjectionEnv(val)

		// 1. Zarejestruj oryginalną nazwę Go (PascalCase) -> np. "TicketID"
		out[field.Name] = normalizedVal

		// 2. Zarejestruj nazwę z tagu JSON (snake_case) -> np. "ticket_id"
		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				out[parts[0]] = normalizedVal
			}
		}

		// 3. Zarejestruj wersję w całości małymi literami -> np. "ticketid"
		out[strings.ToLower(field.Name)] = normalizedVal
	}

	return out
}

var _ = vm.Program{}
