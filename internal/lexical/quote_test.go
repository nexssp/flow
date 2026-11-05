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
