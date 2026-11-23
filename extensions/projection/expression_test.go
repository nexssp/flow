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
		{"array values and nested shorthand", `items: ["one", 2, false, null, { name, enabled: true }]`, `{ items: ["one", 2, false, null, { name: name, enabled: true }] }`},
		{"identifiers in values", `name: title, ready: enabled && active`, `{ name: title, ready: enabled && active }`},
		{"dotted names in values are expressions", `name: person.name`, `{ name: person.name }`},
		{"nested values are expressions", `profile: { name: person.name }`, `{ profile: { name: person.name } }`},
		{"double-quoted data string escapes are decoded", `line: "line\nbreak"`, `{ line: "line\nbreak" }`},
		{"parenthesized expression stays active", `name: (person.name)`, `{ name: (person.name) }`},
		{"composite expression stays active", `name: title + "!"`, `{ name: title + "!" }`},
		{"array contents stay unchanged", `items: [person.name, "x"]`, `{ items: [person.name, "x"] }`},
		{"bare dot", `state: .`, `{ state: __root__ }`},
		{"spread with fields", `..., status: "active"`, `__spread__(__root__, { status: "active" })`},
		{"spread with braced fields", `... { payload: [1] }`, `__spread__(__root__, { payload: [1] })`},
		{"spread braced expression values", `...{ label: status }`, `__spread__(__root__, { label: status })`},
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

func TestProgramCacheRejectsMalformedDirectStringEscapes(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`line: "bad\q"`,
		`line: "bad\xGG"`,
		`line: "bad\uZZZZ"`,
		`line: "\X41"`,
		`line: "\?"`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := newProgramCache().compile(source); err == nil {
				t.Fatalf("compile(%q) succeeded, want invalid escape error", source)
			}
		})
	}
}

func TestBuildProgramSourceWithVisibleNamesInsideBracedSpread(t *testing.T) {
	t.Parallel()
	names := map[string]struct{}{"person": {}, "input": {}, "__root__": {}}
	got, err := buildProgramSourceWithVisibleNames(`...{ bound: person.name, original: input.name, explicit: (person.score), literal: no_binding.path }`, names)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, `__spread__(__root__, { bound: person.name, original: input.name, explicit: (person.score), literal: no_binding.path })`)
}

func TestBuildProgramSourceDistinguishesProjectionPathRoots(t *testing.T) {
	t.Parallel()
	visibleNames := map[string]struct{}{"input": {}, "request": {}, "__root__": {}}
	typedInputs := map[string]struct{}{"request": {}}
	got, err := buildProgramSourceWithVisibleNames(`.request.task, input.request.original, request.current`, visibleNames, typedInputs)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, `{ task: __root__.request.task, original: input.request.original, current: request.current }`)

	_, err = buildProgramSourceWithVisibleNames(`request.current, current: "other"`, visibleNames, typedInputs)
	ktest.RequireCondition(t, err != nil, "expected duplicate key after typed-input path expansion")
	ktest.RequireStringContains(t, err.Error(), `duplicate key "current"`)

	ordinaryBinding, err := buildProgramSourceWithVisibleNames(`current: request.current`, visibleNames)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ordinaryBinding, `{ current: request.current }`)
	ordinaryPath, err := buildProgramSourceWithVisibleNames(`request.current`, visibleNames)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ordinaryPath, `{ request.current }`)
	if _, err := newProgramCache().compileScoped(`request.current`, visibleNames, nil); err == nil {
		t.Fatal("ordinary visible binding unexpectedly enabled named-input path shorthand")
	}
}

func TestProjectionTypedPathCandidateAfterLetsAndSpread(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{raw: `let prefix = "v1"; request.current`, want: true},
		{raw: `... .state, request.current`, want: true},
		{raw: `let prefix = "v1"; ordinary`, want: false},
		{raw: `let prefix = "request.current"; value: 1`, want: false},
	} {
		if got := projectionHasPotentialTypedInputPath(tc.raw); got != tc.want {
			t.Errorf("projectionHasPotentialTypedInputPath(%q) = %t, want %t", tc.raw, got, tc.want)
		}
	}
}

func TestProjectionScannersTreatBackticksAsRaw(t *testing.T) {
	t.Parallel()

	raw := "`prefix\\` + target"
	ktest.RequireEqual(t, skipProjectionQuoted(raw, 0), len("`prefix\\`"))
	ktest.RequireEqual(t, projectionExpressionReferences(raw, "target"), true)

	program, err := buildProgramSource("value: `prefix\\` + `value`, name")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, program, "{ value: `prefix\\` + `value`, name: name }")
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
		{"spread with braced fields", `... { a: 1 }`, "__root__", `a: 1`, true},
		{"spread with adjacent braced fields", `...{ a: 1 }`, "__root__", `a: 1`, true},
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
