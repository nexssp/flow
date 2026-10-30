package projection

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBuildProgramSource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"simple", `name: .name`, `{ name: name }`},
		{"bare-only fields", `name, email`, `{ name: name, email: email }`},
		{"explicit-only fields", `name: .name, email: "fixed"`, `{ name: name, email: "fixed" }`},
		{"mixed fields", `name, email: .email, somethingelse: .canbehere.everything, another, other: .canbe.something`, `{ name: name, email: email, somethingelse: canbehere.everything, another: another, other: canbe.something }`},
		{"nested object field", `profile: { name, email: .email }`, `{ profile: { name: name, email: email } }`},
		{"identifiers in values", `name: title, ready: enabled && active`, `{ name: title, ready: enabled && active }`},
		{"bare dot", `state: .`, `{ state: __root__ }`},
		{"spread with fields", `..., status: "active"`, `__spread__(__root__, { status: "active" })`},
		{"spread with shorthand", `..., name, email: .email`, `__spread__(__root__, { name: name, email: email })`},
		{"bare spread", `...`, `__root__`},
		{"single local binding", `let profile = .user.profile; first: profile.name, display: profile.name`, `let profile = user.profile; { first: profile.name, display: profile.name }`},
		{"multiple local bindings", `let profile = .user.profile; let display = profile.name; first: display, second: profile.name`, `let profile = user.profile; let display = profile.name; { first: display, second: profile.name }`},
		{"local binding with spread", `let profile = .user.profile; ..., display: profile.name`, `let profile = user.profile; __spread__(__root__, { display: profile.name })`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildProgramSource(c.raw)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestBuildProgramSourceRejectsInvalidLocalBindings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"missing initializer", `let profile; display: profile.name`, `expected '=' after local name "profile"`},
		{"missing semicolon", `let profile = .user.profile, display: profile.name`, "missing its terminating ';'"},
		{"empty initializer", `let profile = ; display: profile.name`, `local binding "profile" requires a value`},
		{"duplicate name", `let profile = .user.profile; let profile = .user; display: profile.name`, `duplicate local binding "profile"`},
		{"forward reference", `let display = profile.name; let profile = .user.profile; display: display`, `references later binding "profile"`},
		{"reserved root name", `let __root__ = .user; display: __root__`, `"__root__" is a reserved local name`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildProgramSource(tc.raw)
			ktest.RequireCondition(t, err != nil, "expected local binding error")
			ktest.RequireStringContains(t, err.Error(), tc.want)
		})
	}
}

func TestProjectionLetReferenceValidationIgnoresKeysAndStrings(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`let profile = { name: "name" }; let name = .name; value: profile.name`,
		`let profile = "name"; let name = .name; value: profile`,
		`let profile = .name; let name = .name; value: profile`,
	} {
		if _, err := buildProgramSource(raw); err != nil {
			t.Fatalf("buildProgramSource(%q) returned %v", raw, err)
		}
	}
}

func TestSplitSpread(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		raw       string
		wantState string
		wantRest  string
		wantHas   bool
	}{
		{"no spread", `name: .name`, "", `name: .name`, false},
		{"bare spread", `...`, "__root__", "", true},
		{"spread with fields", `..., a: 1`, "__root__", "a: 1", true},
		{"spread no comma", `... a: 1`, "__root__", "a: 1", true},
		{"looks like spread but identifier", `...name`, "", `...name`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			state, rest, has := splitSpread(c.raw)
			ktest.RequireEqual(t, state, c.wantState)
			ktest.RequireEqual(t, rest, c.wantRest)
			ktest.RequireEqual(t, has, c.wantHas)
		})
	}
}
