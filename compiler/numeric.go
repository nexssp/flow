package compiler

import (
	"errors"
	"fmt"
	"strings"

	exprfile "github.com/expr-lang/expr/file"
	exprlexer "github.com/expr-lang/expr/parser/lexer"
)

var errInvalidNumericSeparator = errors.New("underscores must separate digits or follow a base prefix")

func normalizeNumericSeparators(raw string) (string, error) {
	if !strings.ContainsRune(raw, '_') {
		return raw, nil
	}
	for i := range len(raw) {
		if raw[i] != '_' {
			continue
		}
		if !numericSeparatorIsValid(raw, i) {
			return "", errInvalidNumericSeparator
		}
	}
	return strings.ReplaceAll(raw, "_", ""), nil
}

func numericSeparatorIsValid(raw string, index int) bool {
	if index > 0 && index+1 < len(raw) {
		leftBase := numericLiteralBaseAt(raw, index-1)
		rightBase := numericLiteralBaseAt(raw, index+1)
		if leftBase == rightBase && isNumericDigit(raw[index-1], leftBase) && isNumericDigit(raw[index+1], rightBase) {
			return true
		}
	}

	start := 0
	if strings.HasPrefix(raw, "-") {
		start++
	}
	if index != start+2 || start+1 >= len(raw) || raw[start] != '0' {
		return false
	}
	var base int
	switch raw[start+1] {
	case 'b', 'B':
		base = 2
	case 'o', 'O':
		base = 8
	case 'x', 'X':
		base = 16
	default:
		return false
	}
	return index+1 < len(raw) && isNumericDigit(raw[index+1], base)
}

func numericLiteralBaseAt(raw string, index int) int {
	start := 0
	if strings.HasPrefix(raw, "-") {
		start++
	}
	base := 10
	exponentMarkers := "eE"
	if start+1 < len(raw) && raw[start] == '0' {
		switch raw[start+1] {
		case 'b', 'B':
			base = 2
			exponentMarkers = ""
		case 'o', 'O':
			base = 8
			exponentMarkers = ""
		case 'x', 'X':
			base = 16
			exponentMarkers = "pP"
		}
	}
	if exponentMarkers != "" {
		if exponent := strings.IndexAny(raw[start:], exponentMarkers); exponent >= 0 && index > start+exponent {
			return 10
		}
	}
	return base
}

func isNumericDigit(value byte, base int) bool {
	switch base {
	case 2:
		return value == '0' || value == '1'
	case 8:
		return value >= '0' && value <= '7'
	case 16:
		return isDigit(value) || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
	default:
		return isDigit(value)
	}
}

func validateExpressionNumericSeparators(source string) error {
	if !strings.ContainsRune(source, '_') {
		return nil
	}
	tokens, lexErr := exprlexer.Lex(exprfile.NewSource(source))
	if lexErr == nil {
		for _, token := range tokens {
			if token.Kind != exprlexer.Number || !strings.ContainsRune(token.Value, '_') {
				continue
			}
			if _, err := normalizeNumericSeparators(token.Value); err != nil {
				return fmt.Errorf("invalid numeric separator in literal %q: %w", token.Value, err)
			}
		}
	}
	return nil
}
