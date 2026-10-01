package pool

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMakeStringPathExtractor(t *testing.T) {
	t.Parallel()
	extractor := makeStringPathExtractor(".user.id")

	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"present", map[string]any{"user": map[string]any{"id": "u1"}}, "u1"},
		{"missing", map[string]any{"user": map[string]any{}}, ""},
		{"not a map", "string", ""},
		{"nested missing", map[string]any{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, extractor(c.input), c.want)
		})
	}
}

func TestMakeStringPathExtractor_LeadingDotOptional(t *testing.T) {
	t.Parallel()
	a := makeStringPathExtractor(".user.id")
	b := makeStringPathExtractor("user.id")
	input := map[string]any{"user": map[string]any{"id": "x"}}
	ktest.RequireEqual(t, a(input), b(input))
}
