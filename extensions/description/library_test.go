package description

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestDirective_StoresDescription(t *testing.T) {
	t.Parallel()
	out := map[string]any{}
	_, err := handleDirective(context.Background(), core.DirectiveReq{
		Lines: []string{`@description "coverage fixture"`},
		I:     0,
		Out:   out,
	})
	ktest.RequireNoError(t, err)

	got, ok := out["description"].(string)
	ktest.RequireCondition(t, ok, "description value is %T, want string", out["description"])
	ktest.RequireEqual(t, got, "coverage fixture")
}

func TestDirective_QuoteStyles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"double", `@description "a b"`, "a b"},
		{"single", `@description 'a b'`, "a b"},
		{"unquoted", `@description plain text`, "plain text"},
		{"empty", `@description ""`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out := map[string]any{}
			_, err := handleDirective(context.Background(), core.DirectiveReq{
				Lines: []string{c.input},
				I:     0,
				Out:   out,
			})
			ktest.RequireNoError(t, err)

			got, ok := out["description"].(string)
			ktest.RequireCondition(t, ok, "description value is %T, want string", out["description"])
			ktest.RequireEqual(t, got, c.want)
		})
	}
}

func TestBundle_WiresDirective(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "description")
}
