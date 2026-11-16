package exprsyntax

import "strings"

// IsExpressionPunctuation reports whether lit is one ASCII punctuation rune
// used by expr-lang but not tokenized by the Flow lexer.
func IsExpressionPunctuation(lit string) bool {
	if len(lit) != 1 {
		return false
	}
	return strings.ContainsRune("[];+-*/%^<>", rune(lit[0]))
}
