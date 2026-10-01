package pool

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func runDirective(tb testing.TB, lines ...string) (map[string]any, error) {
	tb.Helper()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "<test>",
	})
	return out, err
}

func TestDirective_BasicDeclaration(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t, `@pool fast [a, b, c]`)
	ktest.RequireNoError(t, err)

	declarations := DeclarationsFromMeta(out)
	ktest.RequireEqual(t, len(declarations), 1)
	ktest.RequireEqual(t, declarations[0].Name, "fast")
	ktest.RequireEqual(t, declarations[0].Members, []string{"a", "b", "c"})
}

func TestDirective_WithStrategy(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@pool workers [a, b] { strategy: "failover" }`,
	)
	ktest.RequireNoError(t, err)

	declarations := DeclarationsFromMeta(out)
	ktest.RequireEqual(t, declarations[0].Strategy, "failover")
}

func TestDirective_MultiLineBlock(t *testing.T) {
	t.Parallel()
	out, err := runDirective(t,
		`@pool workers [a, b] {`,
		`  strategy: "hash"`,
		`  key: ".user.id"`,
		`}`,
	)
	ktest.RequireNoError(t, err)

	declarations := DeclarationsFromMeta(out)
	ktest.RequireEqual(t, declarations[0].Strategy, "hash")
	ktest.RequireEqual(t, declarations[0].Options["key"], ".user.id")
}

func TestDirective_NoBracket(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@pool nope`)
	ktest.RequireErrorContains(t, err, "expected `NAME")
}

func TestDirective_MissingName(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@pool [a, b]`)
	ktest.RequireErrorContains(t, err, "name is required")
}

func TestDirective_EmptyMembers(t *testing.T) {
	t.Parallel()
	_, err := runDirective(t, `@pool empty []`)
	ktest.RequireErrorContains(t, err, "member list is empty")
}

func TestDirective_Duplicate(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	first := core.DirectiveReq{
		Lines: []string{`@pool dup [a]`}, I: 0, Out: out, File: "<test>",
	}
	_, err := handleDirective(context.Background(), first)
	ktest.RequireNoError(t, err)

	second := core.DirectiveReq{
		Lines: []string{`@pool dup [b]`}, I: 0, Out: out, File: "<test>",
	}
	_, err = handleDirective(context.Background(), second)
	ktest.RequireErrorContains(t, err, "duplicate")
}

func TestPoolsFromMeta(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"pools": []Declaration{
			{Name: "a", Members: []string{"x", "y"}},
			{Name: "b", Members: []string{"z"}},
		},
	}
	pools := PoolsFromMeta(meta)
	ktest.RequireEqual(t, pools["a"], []string{"x", "y"})
	ktest.RequireEqual(t, pools["b"], []string{"z"})
}
