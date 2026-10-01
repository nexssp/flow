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
		{"bare dot", `state: .`, `{ state: __root__ }`},
		{"spread with fields", `..., status: "active"`, `__spread__(__root__, { status: "active" })`},
		{"bare spread", `...`, `__root__`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, buildProgramSource(c.raw), c.want)
		})
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

func TestPreprocessDots(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"leading dot", `.name`, `name`},
		{"nested access", `.user.name`, `user.name`},
		{"bare dot", `.`, `__root__`},
		{"member access after ident", `user.name`, `user.name`},
		{"member after call", `fn().name`, `fn().name`},
		{"inside string preserved", `"a.b"`, `"a.b"`},
		{"inside backtick preserved", "`a.b`", "`a.b`"},
		{"mixed", `x: .name, y: .`, `x: name, y: __root__`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, preprocessDots(c.in), c.want)
		})
	}
}

func TestIsMemberAccess(t *testing.T) {
	t.Parallel()
	yes := []byte{'a', 'Z', '9', '_', ')', ']', '#', '@'}
	no := []byte{' ', ',', '.', ':'}
	for _, c := range yes {
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()
			ktest.RequireCondition(t, isMemberAccess(c), "%q should be member access", string(c))
		})
	}
	for _, c := range no {
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()
			ktest.RequireCondition(t, !isMemberAccess(c), "%q should not be member access", string(c))
		})
	}
}

func TestIsIdentStart(t *testing.T) {
	t.Parallel()
	yes := []byte{'a', 'Z', '_'}
	no := []byte{'0', '.', ' '}
	for _, c := range yes {
		ktest.RequireCondition(t, isIdentStart(c), "%q should be ident start", string(c))
	}
	for _, c := range no {
		ktest.RequireCondition(t, !isIdentStart(c), "%q should not be ident start", string(c))
	}
}
