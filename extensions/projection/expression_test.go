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
