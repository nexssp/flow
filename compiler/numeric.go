package compiler

import (
	"fmt"
	"strings"

	exprfile "github.com/expr-lang/expr/file"
	exprlexer "github.com/expr-lang/expr/parser/lexer"

	"github.com/nexssp/flow/internal/lexical"
)

func normalizeNumericSeparators(raw string) (string, error) {
	return lexical.NormalizeNumericSeparators(raw)
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
