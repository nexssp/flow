package compiler

import "testing"

func TestLexer_ComparisonOperatorsAreBenign(t *testing.T) {
	t.Parallel()

	l := NewLexer("a < b > c ! d")
	want := []TokenType{TokenIdent, TokenIdent, TokenIdent, TokenIdent, TokenIdent, TokenIdent, TokenIdent}

	for i, w := range want {
		tok := l.Next()
		if tok.Type != w {
			t.Fatalf("token %d: got %s (%q), want %s", i, tok.Type, tok.Lit, w)
		}
	}
	if final := l.Next(); final.Type != TokenEOF {
		t.Fatalf("expected EOF, got %s", final.Type)
	}
}
