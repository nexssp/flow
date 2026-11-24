package builder

import (
	"strings"
	"sync"
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

func TestTailBufferConcurrentWritersAndReaders(t *testing.T) {
	buffer := newTailBuffer(256)
	var writers sync.WaitGroup
	for _, value := range []byte{'a', 'b'} {
		writers.Add(1)
		go func(value byte) {
			defer writers.Done()
			chunk := strings.Repeat(string(value), 17)
			for range 1000 {
				if n, err := buffer.Write([]byte(chunk)); err != nil || n != len(chunk) {
					t.Errorf("Write() = (%d, %v), want (%d, nil)", n, err, len(chunk))
					return
				}
			}
		}(value)
	}

	var readers sync.WaitGroup
	for range 2 {
		readers.Go(func() {
			for range 1000 {
				_ = buffer.String()
			}
		})
	}
	writers.Wait()
	readers.Wait()

	got := buffer.String()
	const marker = "… (earlier output truncated)\n"
	if !strings.HasPrefix(got, marker) || len(got)-len(marker) != buffer.max {
		t.Fatalf("final tail has unexpected truncation or length: %q", got)
	}
	for _, value := range got[len(marker):] {
		if value != 'a' && value != 'b' {
			t.Fatalf("final tail contains unexpected byte %q", value)
		}
	}
}
