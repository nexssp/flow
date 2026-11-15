package builder

import (
	"strings"
	"testing"
)

func TestTailBufferMarksOnlyDiscardedBytesAsTruncated(t *testing.T) {
	const capacity = 4
	const marker = "… (earlier output truncated)\n"
	for _, tc := range []struct {
		name   string
		writes []string
		want   string
	}{
		{name: "below capacity", writes: []string{"abc"}, want: "abc"},
		{name: "exact capacity", writes: []string{"abcd"}, want: "abcd"},
		{name: "above capacity", writes: []string{"abcde"}, want: marker + "bcde"},
		{name: "successive writes exactly fill capacity", writes: []string{"ab", "cd"}, want: "abcd"},
		{name: "exact-capacity later write discards earlier bytes", writes: []string{"x", "abcd"}, want: marker + "abcd"},
		{name: "successive writes exceed capacity", writes: []string{"abc", "de"}, want: marker + "bcde"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := newTailBuffer(capacity)
			for _, write := range tc.writes {
				n, err := buffer.Write([]byte(write))
				if err != nil || n != len(write) {
					t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", write, n, err, len(write))
				}
			}
			if got := buffer.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
			if strings.Contains(tc.want, marker) != buffer.dropped {
				t.Fatalf("dropped = %t, want %t", buffer.dropped, strings.Contains(tc.want, marker))
			}
		})
	}
}
