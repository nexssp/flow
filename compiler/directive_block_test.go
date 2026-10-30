package compiler

import (
	"strings"
	"testing"
)

func TestParseBlockOptions_SingleLine(t *testing.T) {
	t.Parallel()
	lines := []string{`@pool x [a,b] { strategy: "round_robin", key: ".tid" }`}
	options, next, err := ParseBlockOptions(lines, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 1 {
		t.Fatalf("next = %d, want 1", next)
	}
	if options["strategy"] != "round_robin" {
		t.Errorf("strategy = %q, want round_robin", options["strategy"])
	}
	if options["key"] != ".tid" {
		t.Errorf("key = %q, want .tid", options["key"])
	}
}

func TestParseBlockOptions_MultiLine(t *testing.T) {
	t.Parallel()
	lines := []string{
		`@pool x [a,b] {`,
		`    strategy: "hash"`,
		`    key: ".tenant.id"`,
		`}`,
		`next`,
	}
	options, next, err := ParseBlockOptions(lines, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 4 {
		t.Fatalf("next = %d, want 4", next)
	}
	if options["strategy"] != "hash" || options["key"] != ".tenant.id" {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseBlockOptions_EmptyBlockReturnsNil(t *testing.T) {
	t.Parallel()
	lines := []string{`@pool x [a,b] { }`}
	options, _, err := ParseBlockOptions(lines, 0)
	if err != nil {
		t.Fatal(err)
	}
	if options != nil {
		t.Fatalf("options = %#v, want nil", options)
	}
}

func TestReadBlockWithSpansBalancesNestedBracesAndIgnoresCommentsAndStrings(t *testing.T) {
	lines := []string{
		`@schema Request {`,
		`  ID string`,
		`  Nested struct {`,
		`    Text string`,
		`  }`,
		`  Note string # }`,
		`  Literal string = "}"`,
		`}`,
		`next`,
	}
	block, err := ReadBlockWithSpans(lines, 0)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Join(lines, "\n")
	open := strings.IndexByte(source, '{')
	closingBraceAt := strings.LastIndex(source, "\n}") + 1
	if block.Next != 8 {
		t.Fatalf("next = %d, want 8", block.Next)
	}
	if block.Span != (SourceSpan{Start: open, End: closingBraceAt + 1}) {
		t.Fatalf("block span = %#v, want [%d,%d)", block.Span, open, closingBraceAt+1)
	}
	if block.BodySpan != (SourceSpan{Start: open + 1, End: closingBraceAt}) {
		t.Fatalf("body span = %#v, want [%d,%d)", block.BodySpan, open+1, closingBraceAt)
	}
	if len(block.Body) != 6 {
		t.Fatalf("body entries = %#v, want six lines before the outer closing brace", block.Body)
	}
}

func TestParseList(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line string
		want []string
	}{
		{`@pool x [a, b, c]`, []string{"a", "b", "c"}},
		{`@pool x [single]`, []string{"single"}},
		{`@pool x []`, nil},
		{`@pool x`, nil},
		{`@pool x [a,b] { strategy: "rr" }`, []string{"a", "b"}},
	}
	for _, c := range cases {
		got := ParseList(c.line)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.line, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %v, want %v", c.line, got, c.want)
				break
			}
		}
	}
}
