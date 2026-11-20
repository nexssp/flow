package lexical

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestCanStartQuote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		prefix string
		start  int
		want   bool
	}{
		{name: "at scan start", prefix: "", start: 0, want: true},
		{name: "after whitespace", prefix: " \t\r\n", start: 0, want: true},
		{name: "after colon", prefix: ": \t", start: 0, want: true},
		{name: "after comma", prefix: ",", start: 0, want: true},
		{name: "after bracket", prefix: "[", start: 0, want: true},
		{name: "after brace", prefix: "{", start: 0, want: true},
		{name: "inside bare word", prefix: "value", start: 0},
		{name: "after bare word", prefix: "value ", start: 0},
		{name: "after unrelated punctuation", prefix: "=", start: 0},
		{name: "outside scan boundary", prefix: ":value", start: len(":value"), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := tc.prefix + "'"
			ktest.RequireEqual(t, CanStartQuote(source, len(tc.prefix), tc.start), tc.want)
		})
	}
}

func TestDecodeDoubleQuotedString(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		source string
		want   string
		bad    bool
	}{
		{name: "plain", source: `"plain"`, want: "plain"},
		{name: "newline escape", source: `"line\nbreak"`, want: "line\nbreak"},
		{name: "Unicode braced escape", source: `"\u{41}"`, want: "A"},
		{name: "byte-like escape is a Unicode code point", source: `"\xff"`, want: "ÿ"},
		{name: "escaped quote", source: `"say \"hi\""`, want: `say "hi"`},
		{name: "carriage return follows expr-lang normalization", source: "\"line\rbreak\"", want: "line\nbreak"},
		{name: "unescaped newline rejected", source: "\"line\nbreak\"", bad: true},
		{name: "unknown escape rejected", source: `"bad\q"`, bad: true},
		{name: "malformed hex escape rejected", source: `"bad\xGG"`, bad: true},
		{name: "uppercase X escape rejected by pinned expr-lang", source: `"\X41"`, bad: true},
		{name: "question-mark escape rejected by pinned expr-lang", source: `"\?"`, bad: true},
		{name: "not a complete literal", source: `"plain`, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeDoubleQuotedString(tc.source)
			if tc.bad {
				if err == nil {
					t.Fatalf("DecodeDoubleQuotedString(%q) = %q, want error", tc.source, got)
				}
				return
			}
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, tc.want)
		})
	}
}

var decodeDoubleQuotedBenchmarkSink string

func BenchmarkDecodeDoubleQuotedString(b *testing.B) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "plain", source: `"plain text without escapes"`},
		{name: "escaped", source: `"line\nbreak"`},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				value, err := DecodeDoubleQuotedString(tc.source)
				if err != nil {
					b.Fatal(err)
				}
				decodeDoubleQuotedBenchmarkSink = value
			}
		})
	}
}
