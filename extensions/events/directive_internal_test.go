package events

import (
	"strings"
	"testing"
)

func TestSplitRouteSegmentsTrimsInlineCommentAtEndOfInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		comment string
	}{
		{name: "hash", comment: "# inline note"},
		{name: "slash", comment: "// inline note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "observe sample.events.ready -> sample.sink " + tc.comment
			segments, err := splitRouteSegments(source)
			if err != nil {
				t.Fatalf("split route segments: %v", err)
			}
			if len(segments) == 0 {
				t.Fatal("split route segments returned no route")
			}
			got := strings.TrimSpace(source[segments[0].start:segments[0].end])
			if got != "observe sample.events.ready -> sample.sink" {
				t.Fatalf("first route segment = %q, want comment-free route", got)
			}
		})
	}
}
