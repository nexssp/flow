package projection

import (
	"strings"
	"testing"
)

func TestBuildProgramSource_PathShorthandAndPathSpread(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "path shorthand",
			raw:  `.state.current.attempt.attempt_id, .state.current.attempt.model`,
			want: `{ attempt_id: state.current.attempt.attempt_id, model: state.current.attempt.model }`,
		},
		{
			name: "current-value path spread with explicit override",
			raw:  `... .state, status: "active"`,
			want: `__spread__(state, { status: "active" })`,
		},
		{
			name: "input path spread with explicit override",
			raw:  `...input.state, status: "active"`,
			want: `__spread__(input.state, { status: "active" })`,
		},
		{
			name: "input path spread without overrides",
			raw:  `...input.state`,
			want: `input.state`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildProgramSource(tc.raw)
			if err != nil {
				t.Fatalf("buildProgramSource: %v", err)
			}
			if got != tc.want {
				t.Fatalf("buildProgramSource = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildProgramSourceRejectsWhitespaceBeforeInputPathSpread(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`... input.state`, "...\tinput.state"} {
		if _, err := buildProgramSource(raw); err == nil || !strings.Contains(err.Error(), "no whitespace") {
			t.Errorf("buildProgramSource(%q) error = %v, want input-path spread whitespace error", raw, err)
		}
	}
}

func TestBuildProgramSourceRejectsDuplicateDerivedProjectionKeys(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`.user.id, id: .other`,
		`id: .other, .user.id`,
		`nested: { .user.id, id: .other }`,
	} {
		if _, err := buildProgramSource(raw); err == nil || !strings.Contains(err.Error(), `duplicate key "id"`) {
			t.Errorf("buildProgramSource(%q) error = %v, want duplicate key", raw, err)
		}
	}
}
