package compiler

import "testing"

func TestLexerKeepsNumericSeparatorsInNumberTokens(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"1_123_123", "-1_234.5_6", "1__2", "1_", "1_.2", "1._2"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			lexer := NewLexer(source)
			token := lexer.Next()
			if token.Type != TokNumber || token.Lit != source {
				t.Fatalf("first token = (%v, %q), want TokNumber %q", token.Type, token.Lit, source)
			}
			if next := lexer.Next(); next.Type != TokEOF {
				t.Fatalf("next token = %v, want TokEOF", next.Type)
			}
		})
	}
}

func TestLexerBacktickStringEndingWithBackslash(t *testing.T) {
	t.Parallel()

	source := "`raw\\`"
	lexer := NewLexer(source)
	token := lexer.Next()
	if token.Type != TokString {
		t.Fatalf("token type = %v, want TokString", token.Type)
	}
	if token.Lit != "raw\\" {
		t.Fatalf("token literal = %q, want %q", token.Lit, "raw\\")
	}
	if token.Unterminated {
		t.Fatal("backtick string ending with a backslash was reported as unterminated")
	}
	if token.EndOffset != len(source) {
		t.Fatalf("end offset = %d, want %d", token.EndOffset, len(source))
	}
	if next := lexer.Next(); next.Type != TokEOF {
		t.Fatalf("next token = %v, want TokEOF", next.Type)
	}
}
