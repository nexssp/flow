package compiler

import "testing"

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
